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
	ID       string `json:"id"`
	ArtistId string `json:"artistId"`
	AlbumId  string `json:"albumId"`
}

type PlaylistInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type FrequentArtist struct {
	ID   string
	Name string
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
	songIds := ""
	for i, id := range ids {
		if i > 0 {
			songIds += "&songId="
		}
		songIds += id
	}
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

func generateOnRepeat() {
	if host.GetConfigString("enableOnRepeat", "true") != "true" {
		return
	}
	size := host.GetConfigInt("dailySize", 30)
	artists := getFrequentArtists(5)
	if len(artists) == 0 {
		return
	}
	countPerArtist := size / len(artists) * 3
	if countPerArtist < 10 {
		countPerArtist = 10
	}
	songs := getTopSongsForArtists(artists, countPerArtist)
	if len(songs) < size {
		resp, _ := host.CallSubsonic(fmt.Sprintf("getRandomSongs?size=%d", size*5))
		songs = append(songs, getSongs(resp)...)
	}
	createPlaylist("On Repeat", smartSelect(songs, size))
}

func generateReleaseRadar() {
	if host.GetConfigString("enableReleaseRadar", "true") != "true" {
		return
	}
	size := host.GetConfigInt("weeklySize", 25)

	type albumEntry struct {
		ID       string `json:"id"`
		ArtistId string `json:"artistId"`
	}
	var newestData struct {
		SubsonicResponse struct {
			AlbumList2 struct{ Album []albumEntry `json:"album"` } `json:"albumList2"`
		} `json:"subsonic-response"`
	}
	resp, _ := host.CallSubsonic("getAlbumList2?type=newest&size=50")
	json.Unmarshal([]byte(resp), &newestData)

	// Albums the user has already played — used to promote new additions they started exploring
	var recentData struct {
		SubsonicResponse struct {
			AlbumList2 struct{ Album []albumEntry `json:"album"` } `json:"albumList2"`
		} `json:"subsonic-response"`
	}
	resp, _ = host.CallSubsonic("getAlbumList2?type=recent&size=50")
	json.Unmarshal([]byte(resp), &recentData)
	recentlyPlayedSet := make(map[string]bool)
	for _, a := range recentData.SubsonicResponse.AlbumList2.Album {
		recentlyPlayedSet[a.ID] = true
	}

	frequentArtists := getFrequentArtists(50)
	familiarSet := make(map[string]bool)
	for _, a := range frequentArtists {
		familiarSet[a.ID] = true
	}

	// 3-tier priority:
	// 1. New in library + already played → user is actively exploring it
	// 2. New in library + familiar artist → user knows the artist, hasn't played it yet
	// 3. New in library + unfamiliar artist → pure discovery
	var tier1, tier2, tier3 []string
	for _, a := range newestData.SubsonicResponse.AlbumList2.Album {
		switch {
		case recentlyPlayedSet[a.ID]:
			tier1 = append(tier1, a.ID)
		case familiarSet[a.ArtistId]:
			tier2 = append(tier2, a.ID)
		default:
			tier3 = append(tier3, a.ID)
		}
	}
	prioritized := append(tier1, append(tier2, tier3...)...)

	// Fetch songs album by album. Cap at 25 albums to bound sequential API calls;
	// the size*5 song count exit usually triggers well before that.
	var songs []Song
	for i, albumID := range prioritized {
		if i >= 25 || len(songs) >= size*5 {
			break
		}
		aResp, _ := host.CallSubsonic(fmt.Sprintf("getAlbum?id=%s", albumID))
		var aData struct {
			SubsonicResponse struct {
				Album struct{ Song []Song `json:"song"` } `json:"album"`
			} `json:"subsonic-response"`
		}
		json.Unmarshal([]byte(aResp), &aData)
		songs = append(songs, aData.SubsonicResponse.Album.Song...)
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

	if host.GetConfigString("enableWeeklyDiscovery", "true") == "true" {
		discoverySize := host.GetConfigInt("weeklySize", 25)
		resp, _ = host.CallSubsonic(fmt.Sprintf("getRandomSongs?size=%d", discoverySize*5))
		createPlaylist("Weekly Discovery", smartSelect(getSongs(resp), discoverySize))
	}

	generateReleaseRadar()

	host.KvSet("last_daily_update", time.Now().Format("2006-01-02"))
}

func generateArtistRadio(slot int, artistName, artistId string, size int) {
	pdk.Log(pdk.LogInfo, fmt.Sprintf("Smart Playlist: Generating Artist Radio %d for: %s", slot, artistName))
	slotMarker := fmt.Sprintf("Artist Radio %d:", slot)
	for _, p := range getAllPlaylists() {
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
	createPlaylist(baseName, smartSelect(songs, size))
}

func generateGenreRadio(slot int, genreName string, size int) {
	pdk.Log(pdk.LogInfo, fmt.Sprintf("Smart Playlist: Generating Genre Radio %d for: %s", slot, genreName))
	slotMarker := fmt.Sprintf("Genre Radio %d:", slot)
	for _, p := range getAllPlaylists() {
		if strings.Contains(p.Name, slotMarker) {
			deletePlaylist(p.ID)
		}
	}
	baseName := fmt.Sprintf("Genre Radio %d: %s", slot, genreName)
	poolSize := size * 5
	resp, _ := host.CallSubsonic(fmt.Sprintf("getSongsByGenre?genre=%s&count=%d", url.QueryEscape(genreName), poolSize))
	createPlaylist(baseName, smartSelect(getSongs(resp), size))
}

func GenerateWeeklyMixes() {
	pdk.Log(pdk.LogInfo, "Smart Playlist: Refreshing Weekly Mixes...")
	size := host.GetConfigInt("weeklySize", 25)
	artistRadioSize := host.GetConfigInt("artistRadioSize", 30)

	generateLovedSongsMix()

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
			generateArtistRadio(i+1, artist.Name, artist.ID, artistRadioSize)
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
			generateGenreRadio(i+1, genres[i].Value, size)
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
	onRepeat := host.GetConfigString("enableOnRepeat", "true")
	releaseRadar := host.GetConfigString("enableReleaseRadar", "true")
	lovedMix := host.GetConfigString("enableLovedMix", "true")
	weeklyDisc := host.GetConfigString("enableWeeklyDiscovery", "true")
	artistRadio := host.GetConfigString("enableArtistRadio", "true")
	genreRadio := host.GetConfigString("enableGenreRadio", "true")
	return fmt.Sprintf("%s-%d-%d-%d-%d-%d-%d-%s-%s-%s-%s-%s-%s",
		prefix, ds, ws, ars, dmc, nar, ngr,
		onRepeat, releaseRadar, lovedMix, weeklyDisc, artistRadio, genreRadio)
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
	weeklyDiscoveryEnabled := host.GetConfigString("enableWeeklyDiscovery", "true") == "true"
	artistRadioEnabled := host.GetConfigString("enableArtistRadio", "true") == "true"
	genreRadioEnabled := host.GetConfigString("enableGenreRadio", "true") == "true"

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
			isSmart, baseName, deleteNow = true, "Weekly Discovery", !weeklyDiscoveryEnabled
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
