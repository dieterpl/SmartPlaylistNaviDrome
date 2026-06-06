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

package main

import (
	"fmt"
	"time"

	"navidrome-smart-radio/internal/host"
	"navidrome-smart-radio/internal/playlist"

	"github.com/extism/go-pdk"
)

//export nd_on_init
func nd_on_init() int32 {
	pdk.Log(pdk.LogInfo, "Smart Playlist: Initializing...")
	playlist.GlobalCleanup()
	lastDaily, okD := host.KvGet("last_daily_update")
	lastWeekly, okW := host.KvGet("last_weekly_update")
	lastHash, okH := host.KvGet("config_hash")
	today := time.Now().Format("2006-01-02")
	year, week := time.Now().ISOWeek()
	thisWeek := fmt.Sprintf("%d-W%d", year, week)
	currentHash := playlist.GetConfigHash()
	forceRefresh := !okH || lastHash != currentHash
	if forceRefresh {
		pdk.Log(pdk.LogInfo, "Smart Playlist: Config changed, forcing refresh.")
		host.KvSet("config_hash", currentHash)
	}
	if forceRefresh || !okD || lastDaily != today {
		playlist.GenerateDailyMixes()
	}
	if forceRefresh || !okW || lastWeekly != thisWeek {
		playlist.GenerateWeeklyMixes()
	}
	host.ScheduleRecurring("0 0 * * * *", "{}", "smart_playlist_check")
	return 0
}

//export nd_scheduler_callback
func nd_scheduler_callback() int32 {
	playlist.GlobalCleanup()
	lastDaily, _ := host.KvGet("last_daily_update")
	lastWeekly, _ := host.KvGet("last_weekly_update")
	lastHash, _ := host.KvGet("config_hash")
	today := time.Now().Format("2006-01-02")
	year, week := time.Now().ISOWeek()
	thisWeek := fmt.Sprintf("%d-W%d", year, week)
	currentHash := playlist.GetConfigHash()
	forceRefresh := lastHash != currentHash
	if forceRefresh {
		host.KvSet("config_hash", currentHash)
	}
	if forceRefresh || lastDaily != today {
		playlist.GenerateDailyMixes()
	}
	if forceRefresh || lastWeekly != thisWeek {
		playlist.GenerateWeeklyMixes()
	}
	return 0
}

func main() {}
