// navidrome-smart-playlist - A Navidrome WASM plugin for automatic smart playlist generation
// Copyright (C) 2026 dieterpl
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program. If not, see <http://www.gnu.org/licenses/>.

package playlist

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"sort"
	"strings"
	"time"

	"navidrome-smart-radio/internal/host"

	"github.com/extism/go-pdk"
)

// --- Types ---

type Song struct {
	ID        string `json:"id"`
	ArtistId  string `json:"artistId"`
	AlbumId   string `json:"albumId"`
	PlayCount int    `json:"playCount"`
	Played    string `json:"played"` // ISO 8601 timestamp of last play, or ""
}

type PlaylistInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type FrequentArtist struct {
	ID   string
	Name string
}

type newestAlbumEntry struct {
	ID       string
	ArtistId string
}

// --- Subsonic helpers ---

func getSongs(respJSON string) []Song {
	var data struct {
		SubsonicResponse struct {
			SearchResult3 struct {
				Song []Song `json:"song"`
			} `json:"searchResult3"`
			RandomSongs struct {
				Song []Song `json:"song"`
			} `json:"randomSongs"`
			SongsByGenre struct {
				Song []Song `json:"song"`
			} `json:"songsByGenre"`
			TopSongs struct {
				Song []Song `json:"song"`
			} `json:"topSongs"`
		} `json:"subsonic-response"`
	}
	json.Unmarshal([]byte(respJSON), &data)
	sr := data.SubsonicResponse
	var songs []Song
	songs = append(songs, sr.SearchResult3.Song...)
	songs = append(songs, sr.RandomSongs.Song...)
	songs = append(songs, sr.SongsByGenre.Song...)
	songs = append(songs, sr.TopSongs.Song...)
	return songs
}

func getFrequentArtists(maxCount int) []FrequentArtist {
	resp, _ := host.CallSubsonic("getAlbumList2?type=frequent&size=200")
	var data struct {
		SubsonicResponse struct {
			AlbumList2 struct {
				Album []struct {
					ArtistId string `json:"artistId"`
					Artist   string `json:"artist"`
				} `json:"album"`
			} `json:"albumList2"`
		} `json:"subsonic-response"`
	}
	json.Unmarshal([]byte(resp), &data)
	seen := make(map[string]bool)
	var artists []FrequentArtist
	for _, a := range data.SubsonicResponse.AlbumList2.Album {
		if a.ArtistId == "" || seen[a.ArtistId] {
			continue
		}
		seen[a.ArtistId] = true
		artists = append(artists, FrequentArtist{ID: a.ArtistId, Name: a.Artist})
		if len(artists) >= maxCount {
			break
		}
	}
	return artists
}

func getTopSongsForArtists(artists []FrequentArtist, countPerArtist int) []Song {
	var all []Song
	for _, artist := range artists {
		resp, _ := host.CallSubsonic(fmt.Sprintf("getTopSongs?artist=%s&count=%d", url.QueryEscape(artist.Name), countPerArtist))
		songs := getSongs(resp)
		if len(songs) == 0 {
			resp, _ = host.CallSubsonic(fmt.Sprintf("search3?query=%s&songCount=%d", url.QueryEscape(artist.Name), countPerArtist))
			songs = getSongs(resp)
		}
		all = append(all, songs...)
	}
	return all
}

func getAllPlaylists() []PlaylistInfo {
	resp, _ := host.CallSubsonic("getPlaylists")
	var data struct {
		SubsonicResponse struct {
			Playlists struct {
				Playlist []PlaylistInfo `json:"playlist"`
			} `json:"playlists"`
		} `json:"subsonic-response"`
	}
	json.Unmarshal([]byte(resp), &data)
	return data.SubsonicResponse.Playlists.Playlist
}

func deletePlaylist(id string) {
	host.CallSubsonic(fmt.Sprintf("deletePlaylist?id=%s", id))
}

func findPlaylistIDs(baseName string) []string {
	var ids []string
	for _, p := range getAllPlaylists() {
		if strings.HasSuffix(p.Name, baseName) {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

func createPlaylist(baseName string, ids []string) {
	if len(ids) == 0 {
		return
	}
	prefix := host.GetConfigString("prefix", "✨ ")
	fullName := prefix + baseName
	songIds := buildSongParam(ids)
	existingIds := findPlaylistIDs(baseName)
	var uri string
	if len(existingIds) > 0 {
		mainId := existingIds[0]
		for i := 1; i < len(existingIds); i++ {
			deletePlaylist(existingIds[i])
		}
		uri = fmt.Sprintf("createPlaylist?playlistId=%s&name=%s&songId=%s", mainId, url.QueryEscape(fullName), songIds)
	} else {
		uri = fmt.Sprintf("createPlaylist?name=%s&songId=%s", url.QueryEscape(fullName), songIds)
	}
	host.CallSubsonic(uri)
}

func fetchNewestAlbums(count int) []newestAlbumEntry {
	type entry struct {
		ID       string `json:"id"`
		ArtistId string `json:"artistId"`
	}
	var data struct {
		SubsonicResponse struct {
			AlbumList2 struct{ Album []entry `json:"album"` } `json:"albumList2"`
		} `json:"subsonic-response"`
	}
	resp, _ := host.CallSubsonic(fmt.Sprintf("getAlbumList2?type=newest&size=%d", count))
	json.Unmarshal([]byte(resp), &data)
	var result []newestAlbumEntry
	for _, a := range data.SubsonicResponse.AlbumList2.Album {
		result = append(result, newestAlbumEntry{ID: a.ID, ArtistId: a.ArtistId})
	}
	return result
}

func buildSongParam(ids []string) string {
	param := ""
	for i, id := range ids {
		if i > 0 {
			param += "&songId="
		}
		param += id
	}
	return param
}

func smartSelect(songs []Song, targetSize int) []string {
	grouped := make(map[string]map[string][]Song)
	for _, s := range songs {
		artist := s.ArtistId
		if artist == "" {
			artist = "unknown"
		}
		album := s.AlbumId
		if album == "" {
			album = "unknown"
		}
		if grouped[artist] == nil {
			grouped[artist] = make(map[string][]Song)
		}
		grouped[artist][album] = append(grouped[artist][album], s)
	}

	var artists []string
	for a := range grouped {
		artists = append(artists, a)
	}
	rand.Shuffle(len(artists), func(i, j int) { artists[i], artists[j] = artists[j], artists[i] })

	var result []string
	addedSet := make(map[string]bool)
	artistIdx := 0

	for len(result) < targetSize && len(grouped) > 0 {
		artist := artists[artistIdx]
		albumsMap := grouped[artist]

		var albums []string
		for al := range albumsMap {
			albums = append(albums, al)
		}

		if len(albums) == 0 {
			delete(grouped, artist)
			artists = append(artists[:artistIdx], artists[artistIdx+1:]...)
			if len(artists) == 0 {
				break
			}
			if artistIdx >= len(artists) {
				artistIdx = 0
			}
			continue
		}

		rand.Shuffle(len(albums), func(i, j int) { albums[i], albums[j] = albums[j], albums[i] })
		album := albums[0]

		songList := albumsMap[album]
		var selectedSong *Song
		if len(songList) > 0 {
			selectedSong = &songList[0]
			grouped[artist][album] = songList[1:]
			if len(grouped[artist][album]) == 0 {
				delete(grouped[artist], album)
			}
		}

		if selectedSong != nil && !addedSet[selectedSong.ID] {
			result = append(result, selectedSong.ID)
			addedSet[selectedSong.ID] = true
		}

		artistIdx++
		if artistIdx >= len(artists) {
			artistIdx = 0
		}
	}

	return result
}

// --- Generators ---

func generateDailyDiscovery(familiarSet map[string]bool, newest []newestAlbumEntry) {
	if host.GetConfigString("enableDailyDiscovery", "true") != "true" {
		return
	}
	size := host.GetConfigInt("dailySize", 30)
	poolTarget := size * 5
	perAlbumCap := size / 8
	if perAlbumCap < 2 {
		perAlbumCap = 2
	}

	// Split recently-added albums into familiar vs. discovery tiers
	var tier1, tier2 []string
	for _, a := range newest {
		if familiarSet[a.ArtistId] {
			tier1 = append(tier1, a.ID)
		} else {
			tier2 = append(tier2, a.ID)
		}
	}

	fetchAlbumSongs := func(albumID string) []Song {
		aResp, _ := host.CallSubsonic(fmt.Sprintf("getAlbum?id=%s", albumID))
		var aData struct {
			SubsonicResponse struct {
				Album struct{ Song []Song `json:"song"` } `json:"album"`
			} `json:"subsonic-response"`
		}
		json.Unmarshal([]byte(aResp), &aData)
		return aData.SubsonicResponse.Album.Song
	}

	appendCapped := func(songs []Song, albumID string) []Song {
		albumSongs := fetchAlbumSongs(albumID)
		rand.Shuffle(len(albumSongs), func(i, j int) { albumSongs[i], albumSongs[j] = albumSongs[j], albumSongs[i] })
		if len(albumSongs) > perAlbumCap {
			albumSongs = albumSongs[:perAlbumCap]
		}
		return append(songs, albumSongs...)
	}

	var songs []Song

	// Tier 1: familiar artists from recent additions (up to 20 album fetches)
	tier1Fetched := 0
	for _, albumID := range tier1 {
		if tier1Fetched >= 20 || len(songs) >= poolTarget {
			break
		}
		songs = appendCapped(songs, albumID)
		tier1Fetched++
	}

	// Tier 2: discovery — unfamiliar artists from recent additions (up to 10 album fetches)
	tier2Fetched := 0
	for _, albumID := range tier2 {
		if tier2Fetched >= 10 || len(songs) >= poolTarget {
			break
		}
		songs = appendCapped(songs, albumID)
		tier2Fetched++
	}

	// Pad with random songs to reach pool target
	if len(songs) < poolTarget {
		padResp, _ := host.CallSubsonic(fmt.Sprintf("getRandomSongs?size=%d", poolTarget-len(songs)))
		songs = append(songs, getSongs(padResp)...)
	}

	createPlaylist("Daily Discovery", smartSelect(songs, size))
}

func generateForgottenFavorites() {
	if host.GetConfigString("enableForgottenFavorites", "true") != "true" {
		return
	}
	thresholdDays := host.GetConfigInt("forgottenThresholdDays", 180)
	cutoff := time.Now().AddDate(0, 0, -thresholdDays)
	size := host.GetConfigInt("weeklySize", 25)

	resp, _ := host.CallSubsonic("getStarred2")
	var data struct {
		SubsonicResponse struct {
			Starred2 struct {
				Song []Song `json:"song"`
			} `json:"starred2"`
		} `json:"subsonic-response"`
	}
	json.Unmarshal([]byte(resp), &data)

	var candidates []Song
	for _, s := range data.SubsonicResponse.Starred2.Song {
		if s.Played == "" {
			continue
		}
		// RFC3339Nano handles sub-second precision (Navidrome emits e.g. "2024-01-15T10:30:00.000Z");
		// fall back to RFC3339 for timestamps without fractional seconds.
		t, err := time.Parse(time.RFC3339Nano, s.Played)
		if err != nil {
			t, err = time.Parse(time.RFC3339, s.Played)
		}
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			candidates = append(candidates, s)
		}
	}

	if len(candidates) == 0 {
		return
	}
	createPlaylist("Forgotten Favorites", smartSelect(candidates, size))
}

func filterByRecency(songs []Song, cutoff time.Time) []Song {
	var out []Song
	for _, s := range songs {
		if s.Played == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, s.Played)
		if err != nil {
			t, err = time.Parse(time.RFC3339, s.Played)
		}
		if err == nil && t.After(cutoff) {
			out = append(out, s)
		}
	}
	return out
}

func generateOnRepeat() {
	if host.GetConfigString("enableOnRepeat", "true") != "true" {
		return
	}
	size := host.GetConfigInt("dailySize", 30)
	recentDays := host.GetConfigInt("onRepeatRecentDays", 90)
	cutoff := time.Now().AddDate(0, 0, -recentDays)

	artists := getFrequentArtists(5)
	if len(artists) == 0 {
		return
	}
	countPerArtist := size / len(artists) * 3
	if countPerArtist < 10 {
		countPerArtist = 10
	}
	songs := filterByRecency(getTopSongsForArtists(artists, countPerArtist), cutoff)

	// Pad with songs from recently-played albums (still filtered by recency).
	if len(songs) < size {
		resp, _ := host.CallSubsonic(fmt.Sprintf("getAlbumList2?type=recent&size=%d", size))
		var recentData struct {
			SubsonicResponse struct {
				AlbumList2 struct {
					Album []struct {
						ID string `json:"id"`
					} `json:"album"`
				} `json:"albumList2"`
			} `json:"subsonic-response"`
		}
		json.Unmarshal([]byte(resp), &recentData)
		seen := make(map[string]bool)
		for _, s := range songs {
			seen[s.ID] = true
		}
		for _, alb := range recentData.SubsonicResponse.AlbumList2.Album {
			aResp, _ := host.CallSubsonic(fmt.Sprintf("getAlbum?id=%s", alb.ID))
			var aData struct {
				SubsonicResponse struct {
					Album struct {
						Song []Song `json:"song"`
					} `json:"album"`
				} `json:"subsonic-response"`
			}
			json.Unmarshal([]byte(aResp), &aData)
			for _, s := range filterByRecency(aData.SubsonicResponse.Album.Song, cutoff) {
				if !seen[s.ID] {
					seen[s.ID] = true
					songs = append(songs, s)
				}
			}
			if len(songs) >= size*3 {
				break
			}
		}
	}
	createPlaylist("On Repeat", smartSelect(songs, size))
}

func generateReleaseRadar(familiarSet map[string]bool, newest []newestAlbumEntry) {
	if host.GetConfigString("enableReleaseRadar", "true") != "true" {
		return
	}
	size := host.GetConfigInt("weeklySize", 25)

	// Albums the user has already played — used to promote new additions they started exploring
	type albumIDEntry struct {
		ID string `json:"id"`
	}
	var recentData struct {
		SubsonicResponse struct {
			AlbumList2 struct{ Album []albumIDEntry `json:"album"` } `json:"albumList2"`
		} `json:"subsonic-response"`
	}
	resp, _ := host.CallSubsonic("getAlbumList2?type=recent&size=50")
	json.Unmarshal([]byte(resp), &recentData)
	recentlyPlayedSet := make(map[string]bool)
	for _, a := range recentData.SubsonicResponse.AlbumList2.Album {
		recentlyPlayedSet[a.ID] = true
	}

	// 3-tier priority:
	// 1. New in library + already played → user is actively exploring it
	// 2. New in library + familiar artist → user knows the artist, hasn't played it yet
	// 3. New in library + unfamiliar artist → pure discovery
	var tier1, tier2, tier3 []string
	for _, a := range newest {
		switch {
		case recentlyPlayedSet[a.ID]:
			tier1 = append(tier1, a.ID)
		case familiarSet[a.ArtistId]:
			tier2 = append(tier2, a.ID)
		default:
			tier3 = append(tier3, a.ID)
		}
	}

	// Cap each tier's album count so tier3 (pure discovery) always gets representation.
	// Total max = 15+15+10 = 40 album fetches.
	const tier12Cap, tier3Cap = 15, 10
	if len(tier1) > tier12Cap {
		tier1 = tier1[:tier12Cap]
	}
	if len(tier2) > tier12Cap {
		tier2 = tier2[:tier12Cap]
	}
	if len(tier3) > tier3Cap {
		tier3 = tier3[:tier3Cap]
	}
	prioritized := append(tier1, append(tier2, tier3...)...)

	// Fetch songs album by album; take at most perAlbumCap songs per album (shuffled)
	// so no single long album dominates the pool.
	perAlbumCap := size / 6
	if perAlbumCap < 3 {
		perAlbumCap = 3
	}
	poolTarget := size * 5
	var songs []Song
	for _, albumID := range prioritized {
		if len(songs) >= poolTarget {
			break
		}
		aResp, _ := host.CallSubsonic(fmt.Sprintf("getAlbum?id=%s", albumID))
		var aData struct {
			SubsonicResponse struct {
				Album struct{ Song []Song `json:"song"` } `json:"album"`
			} `json:"subsonic-response"`
		}
		json.Unmarshal([]byte(aResp), &aData)
		albumSongs := aData.SubsonicResponse.Album.Song
		rand.Shuffle(len(albumSongs), func(i, j int) { albumSongs[i], albumSongs[j] = albumSongs[j], albumSongs[i] })
		if len(albumSongs) > perAlbumCap {
			albumSongs = albumSongs[:perAlbumCap]
		}
		songs = append(songs, albumSongs...)
	}
	if len(songs) < size {
		resp, _ = host.CallSubsonic(fmt.Sprintf("getRandomSongs?size=%d", size*5))
		songs = append(songs, getSongs(resp)...)
	}
	createPlaylist("Release Radar", smartSelect(songs, size))
}

func generateLovedSongsMix() {
	if host.GetConfigString("enableLovedMix", "true") != "true" {
		return
	}
	size := host.GetConfigInt("weeklySize", 25)
	resp, _ := host.CallSubsonic("getStarred2")
	var data struct {
		SubsonicResponse struct {
			Starred2 struct {
				Song []Song `json:"song"`
			} `json:"starred2"`
		} `json:"subsonic-response"`
	}
	json.Unmarshal([]byte(resp), &data)
	songs := data.SubsonicResponse.Starred2.Song
	if len(songs) == 0 {
		return
	}
	rand.Shuffle(len(songs), func(i, j int) { songs[i], songs[j] = songs[j], songs[i] })
	createPlaylist("Your Loved Songs Mix", smartSelect(songs, size))
}

func GenerateDailyMixes() {
	pdk.Log(pdk.LogInfo, "Smart Playlist: Refreshing Daily Mixes...")
	size := host.GetConfigInt("dailySize", 30)
	poolSize := size * 5
	count := host.GetConfigInt("dailyMixCount", 3)
	if count < 1 {
		count = 1
	}
	if count > 6 {
		count = 6
	}

	type albumEntry struct {
		Genre string `json:"genre"`
	}
	var albumResp struct {
		SubsonicResponse struct {
			AlbumList2 struct {
				Album []albumEntry `json:"album"`
			} `json:"albumList2"`
		} `json:"subsonic-response"`
	}

	seen := make(map[string]bool)
	var orderedGenres []string

	resp, _ := host.CallSubsonic("getAlbumList2?type=recent&size=100")
	json.Unmarshal([]byte(resp), &albumResp)
	for _, a := range albumResp.SubsonicResponse.AlbumList2.Album {
		if a.Genre != "" && !seen[a.Genre] {
			seen[a.Genre] = true
			orderedGenres = append(orderedGenres, a.Genre)
		}
	}

	resp, _ = host.CallSubsonic("getAlbumList2?type=frequent&size=100")
	json.Unmarshal([]byte(resp), &albumResp)
	for _, a := range albumResp.SubsonicResponse.AlbumList2.Album {
		if a.Genre != "" && !seen[a.Genre] {
			seen[a.Genre] = true
			orderedGenres = append(orderedGenres, a.Genre)
		}
	}

	for i := 0; i < count; i++ {
		mixNum := i + 1
		var songs []Song
		if i < len(orderedGenres) {
			genre := orderedGenres[i]
			resp, _ = host.CallSubsonic(fmt.Sprintf("getSongsByGenre?genre=%s&count=%d", url.QueryEscape(genre), poolSize))
			songs = getSongs(resp)
			if len(songs) < size {
				resp, _ = host.CallSubsonic(fmt.Sprintf("getRandomSongs?size=%d", poolSize-len(songs)))
				songs = append(songs, getSongs(resp)...)
			}
		} else {
			resp, _ = host.CallSubsonic(fmt.Sprintf("getRandomSongs?size=%d", poolSize))
			songs = getSongs(resp)
		}
		createPlaylist(fmt.Sprintf("Daily Mix %d", mixNum), smartSelect(songs, size))
	}

	generateOnRepeat()

	// Pre-compute shared inputs only when at least one consumer is enabled.
	dailyDiscEnabled := host.GetConfigString("enableDailyDiscovery", "true") == "true"
	releaseRadarEnabled := host.GetConfigString("enableReleaseRadar", "true") == "true"
	if dailyDiscEnabled || releaseRadarEnabled {
		frequentArtists := getFrequentArtists(50)
		sharedFamiliarSet := make(map[string]bool)
		for _, a := range frequentArtists {
			sharedFamiliarSet[a.ID] = true
		}
		sharedNewest := fetchNewestAlbums(100)
		generateDailyDiscovery(sharedFamiliarSet, sharedNewest)
		generateReleaseRadar(sharedFamiliarSet, sharedNewest)
	}

	host.KvSet("last_daily_update", time.Now().Format("2006-01-02"))
}

func generateArtistRadio(slot int, artistName, artistId string, size int, allPlaylists []PlaylistInfo) {
	pdk.Log(pdk.LogInfo, fmt.Sprintf("Smart Playlist: Generating Artist Radio %d for: %s", slot, artistName))
	slotMarker := fmt.Sprintf("Artist Radio %d:", slot)
	for _, p := range allPlaylists {
		if strings.Contains(p.Name, slotMarker) {
			deletePlaylist(p.ID)
		}
	}
	baseName := fmt.Sprintf("Artist Radio %d: %s", slot, artistName)
	poolSize := size * 5
	resp, _ := host.CallSubsonic(fmt.Sprintf("getTopSongs?artist=%s&count=%d", url.QueryEscape(artistName), poolSize))
	songs := getSongs(resp)
	if len(songs) < size && artistId != "" {
		resp, _ = host.CallSubsonic(fmt.Sprintf("getArtist?id=%s", artistId))
		var artistData struct {
			SubsonicResponse struct {
				Artist struct {
					Album []struct {
						ID string `json:"id"`
					} `json:"album"`
				} `json:"artist"`
			} `json:"subsonic-response"`
		}
		json.Unmarshal([]byte(resp), &artistData)
		for _, alb := range artistData.SubsonicResponse.Artist.Album {
			aResp, _ := host.CallSubsonic(fmt.Sprintf("getAlbum?id=%s", alb.ID))
			var aData struct {
				SubsonicResponse struct {
					Album struct {
						Song []Song `json:"song"`
					} `json:"album"`
				} `json:"subsonic-response"`
			}
			json.Unmarshal([]byte(aResp), &aData)
			songs = append(songs, aData.SubsonicResponse.Album.Song...)
			if len(songs) >= poolSize {
				break
			}
		}
	}
	// Keep only songs that actually belong to this artist to prevent cross-artist contamination.
	if artistId != "" {
		var filtered []Song
		for _, s := range songs {
			if s.ArtistId == artistId {
				filtered = append(filtered, s)
			}
		}
		songs = filtered
	}
	selected := smartSelect(songs, size)
	if len(selected) == 0 {
		return
	}
	prefix := host.GetConfigString("prefix", "✨ ")
	fullName := prefix + baseName
	host.CallSubsonic(fmt.Sprintf("createPlaylist?name=%s&songId=%s", url.QueryEscape(fullName), buildSongParam(selected)))
}

func generateGenreRadio(slot int, genreName string, size int, allPlaylists []PlaylistInfo) {
	pdk.Log(pdk.LogInfo, fmt.Sprintf("Smart Playlist: Generating Genre Radio %d for: %s", slot, genreName))
	slotMarker := fmt.Sprintf("Genre Radio %d:", slot)
	for _, p := range allPlaylists {
		if strings.Contains(p.Name, slotMarker) {
			deletePlaylist(p.ID)
		}
	}
	baseName := fmt.Sprintf("Genre Radio %d: %s", slot, genreName)
	poolSize := size * 5
	resp, _ := host.CallSubsonic(fmt.Sprintf("getSongsByGenre?genre=%s&count=%d", url.QueryEscape(genreName), poolSize))
	selected := smartSelect(getSongs(resp), size)
	if len(selected) == 0 {
		return
	}
	prefix := host.GetConfigString("prefix", "✨ ")
	fullName := prefix + baseName
	host.CallSubsonic(fmt.Sprintf("createPlaylist?name=%s&songId=%s", url.QueryEscape(fullName), buildSongParam(selected)))
}

func GenerateWeeklyMixes() {
	pdk.Log(pdk.LogInfo, "Smart Playlist: Refreshing Weekly Mixes...")
	size := host.GetConfigInt("weeklySize", 25)
	artistRadioSize := host.GetConfigInt("artistRadioSize", 30)

	generateLovedSongsMix()
	generateForgottenFavorites()

	// Fetch playlist list once; pass it into radio generators to avoid N+1 getPlaylists calls.
	radioPlaylists := getAllPlaylists()

	if host.GetConfigString("enableArtistRadio", "true") == "true" {
		numArtistRadios := host.GetConfigInt("numArtistRadios", 5)
		if numArtistRadios < 1 {
			numArtistRadios = 1
		}
		if numArtistRadios > 20 {
			numArtistRadios = 20
		}
		artists := getFrequentArtists(numArtistRadios)
		for i, artist := range artists {
			generateArtistRadio(i+1, artist.Name, artist.ID, artistRadioSize, radioPlaylists)
		}
	}

	if host.GetConfigString("enableGenreRadio", "true") == "true" {
		numGenreRadios := host.GetConfigInt("numGenreRadios", 3)
		if numGenreRadios < 1 {
			numGenreRadios = 1
		}
		if numGenreRadios > 10 {
			numGenreRadios = 10
		}
		resp, _ := host.CallSubsonic("getGenres")
		var genreData struct {
			SubsonicResponse struct {
				Genres struct {
					Genre []struct {
						Value     string `json:"value"`
						SongCount int    `json:"songCount"`
					} `json:"genre"`
				} `json:"genres"`
			} `json:"subsonic-response"`
		}
		json.Unmarshal([]byte(resp), &genreData)
		genres := genreData.SubsonicResponse.Genres.Genre
		sort.Slice(genres, func(i, j int) bool {
			return genres[i].SongCount > genres[j].SongCount
		})
		for i := 0; i < numGenreRadios && i < len(genres); i++ {
			generateGenreRadio(i+1, genres[i].Value, size, radioPlaylists)
		}
	}

	year, week := time.Now().ISOWeek()
	host.KvSet("last_weekly_update", fmt.Sprintf("%d-W%d", year, week))
}

func GetConfigHash() string {
	prefix := host.GetConfigString("prefix", "✨ ")
	ds := host.GetConfigInt("dailySize", 30)
	ws := host.GetConfigInt("weeklySize", 25)
	ars := host.GetConfigInt("artistRadioSize", 30)
	dmc := host.GetConfigInt("dailyMixCount", 3)
	nar := host.GetConfigInt("numArtistRadios", 5)
	ngr := host.GetConfigInt("numGenreRadios", 3)
	ftd := host.GetConfigInt("forgottenThresholdDays", 180)
	ord := host.GetConfigInt("onRepeatRecentDays", 90)
	onRepeat := host.GetConfigString("enableOnRepeat", "true")
	releaseRadar := host.GetConfigString("enableReleaseRadar", "true")
	lovedMix := host.GetConfigString("enableLovedMix", "true")
	dailyDisc := host.GetConfigString("enableDailyDiscovery", "true")
	artistRadio := host.GetConfigString("enableArtistRadio", "true")
	genreRadio := host.GetConfigString("enableGenreRadio", "true")
	forgotten := host.GetConfigString("enableForgottenFavorites", "true")
	return fmt.Sprintf("%s-%d-%d-%d-%d-%d-%d-%d-%d-%s-%s-%s-%s-%s-%s-%s",
		prefix, ds, ws, ars, dmc, nar, ngr, ftd, ord,
		onRepeat, releaseRadar, lovedMix, dailyDisc, artistRadio, genreRadio, forgotten)
}

func GlobalCleanup() {
	pdk.Log(pdk.LogInfo, "Smart Playlist: Performing global cleanup...")

	dailyMixCount := host.GetConfigInt("dailyMixCount", 3)
	if dailyMixCount < 1 {
		dailyMixCount = 1
	}
	if dailyMixCount > 6 {
		dailyMixCount = 6
	}
	numArtistRadios := host.GetConfigInt("numArtistRadios", 5)
	if numArtistRadios < 1 {
		numArtistRadios = 1
	}
	if numArtistRadios > 20 {
		numArtistRadios = 20
	}
	numGenreRadios := host.GetConfigInt("numGenreRadios", 3)
	if numGenreRadios < 1 {
		numGenreRadios = 1
	}
	if numGenreRadios > 10 {
		numGenreRadios = 10
	}
	onRepeatEnabled := host.GetConfigString("enableOnRepeat", "true") == "true"
	releaseRadarEnabled := host.GetConfigString("enableReleaseRadar", "true") == "true"
	lovedMixEnabled := host.GetConfigString("enableLovedMix", "true") == "true"
	dailyDiscoveryEnabled := host.GetConfigString("enableDailyDiscovery", "true") == "true"
	artistRadioEnabled := host.GetConfigString("enableArtistRadio", "true") == "true"
	genreRadioEnabled := host.GetConfigString("enableGenreRadio", "true") == "true"
	forgottenEnabled := host.GetConfigString("enableForgottenFavorites", "true") == "true"

	playlists := getAllPlaylists()
	seenBaseNames := make(map[string]string)

	for _, p := range playlists {
		isSmart := false
		baseName := ""
		deleteNow := false

		switch {
		case strings.Contains(p.Name, "Daily Mix "):
			isSmart = true
			for i := 1; i <= 6; i++ {
				slotStr := fmt.Sprintf("Daily Mix %d", i)
				if strings.HasSuffix(p.Name, slotStr) {
					baseName = slotStr
					deleteNow = i > dailyMixCount
					break
				}
			}
		case strings.HasSuffix(p.Name, "Weekly Discovery"):
			isSmart, baseName, deleteNow = true, "Weekly Discovery", true
		case strings.HasSuffix(p.Name, "Daily Discovery"):
			isSmart, baseName, deleteNow = true, "Daily Discovery", !dailyDiscoveryEnabled
		case strings.HasSuffix(p.Name, "Forgotten Favorites"):
			isSmart, baseName, deleteNow = true, "Forgotten Favorites", !forgottenEnabled
		case strings.HasSuffix(p.Name, "On Repeat"):
			isSmart, baseName, deleteNow = true, "On Repeat", !onRepeatEnabled
		case strings.HasSuffix(p.Name, "Release Radar"):
			isSmart, baseName, deleteNow = true, "Release Radar", !releaseRadarEnabled
		case strings.HasSuffix(p.Name, "Your Loved Songs Mix"):
			isSmart, baseName, deleteNow = true, "Your Loved Songs Mix", !lovedMixEnabled
		case strings.Contains(p.Name, "Artist Radio "):
			isSmart = true
			for i := 1; i <= 20; i++ {
				slotStr := fmt.Sprintf("Artist Radio %d:", i)
				if strings.Contains(p.Name, slotStr) {
					baseName = fmt.Sprintf("Artist Radio %d", i)
					deleteNow = !artistRadioEnabled || i > numArtistRadios
					break
				}
			}
		case strings.Contains(p.Name, "Genre Radio "):
			isSmart = true
			for i := 1; i <= 10; i++ {
				slotStr := fmt.Sprintf("Genre Radio %d:", i)
				if strings.Contains(p.Name, slotStr) {
					baseName = fmt.Sprintf("Genre Radio %d", i)
					deleteNow = !genreRadioEnabled || i > numGenreRadios
					break
				}
			}
		}

		if !isSmart || baseName == "" {
			continue
		}
		if deleteNow {
			deletePlaylist(p.ID)
			continue
		}
		if _, ok := seenBaseNames[baseName]; ok {
			deletePlaylist(p.ID)
		} else {
			seenBaseNames[baseName] = p.ID
		}
	}
}
