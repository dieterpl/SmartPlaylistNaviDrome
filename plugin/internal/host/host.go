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

package host

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/extism/go-pdk"
)

//go:wasmimport extism:host/user subsonicapi_call
func subsonicapi_call(ptr uint64) uint64

//go:wasmimport extism:host/user scheduler_schedulerecurring
func scheduler_schedulerecurring(ptr uint64) uint64

//go:wasmimport extism:host/user kvstore_set
func kvstore_set(ptr uint64) uint64

//go:wasmimport extism:host/user kvstore_get
func kvstore_get(ptr uint64) uint64

//go:wasmimport extism:host/user config_get
func config_get(ptr uint64) uint64

//go:wasmimport extism:host/user config_getint
func config_getint(ptr uint64) uint64

//go:wasmimport extism:host/user users_getusers
func users_getusers(ptr uint64) uint64

type subsonicCallReq struct {
	Uri string `json:"uri"`
}

type subsonicCallResp struct {
	ResponseJSON string `json:"responseJson,omitempty"`
	Error        string `json:"error,omitempty"`
}

type kvSetReq struct {
	Key   string `json:"key"`
	Value []byte `json:"value"`
}

type kvGetReq struct {
	Key string `json:"key"`
}

type kvGetResp struct {
	Value  []byte `json:"value,omitempty"`
	Exists bool   `json:"exists,omitempty"`
	Error  string `json:"error,omitempty"`
}

type configReq struct {
	Key string `json:"key"`
}

type configStrResp struct {
	Value  string `json:"value,omitempty"`
	Exists bool   `json:"exists,omitempty"`
}

type configIntResp struct {
	Value  int64 `json:"value,omitempty"`
	Exists bool  `json:"exists,omitempty"`
}

type userEntry struct {
	UserName string `json:"userName"`
	IsAdmin  bool   `json:"isAdmin"`
}

type usersResp struct {
	Result []userEntry `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

var activeUsername string

func GetUserSettings() string {
	reqBytes, _ := json.Marshal(configReq{Key: "userSettings"})
	mem := pdk.AllocateBytes(reqBytes)
	resMem := pdk.FindMemory(config_get(mem.Offset()))
	var res configStrResp
	json.Unmarshal(resMem.ReadBytes(), &res)
	if res.Exists {
		return res.Value
	}
	return "[]"
}

func GetActiveUsername() string {
	if activeUsername != "" {
		return activeUsername
	}
	mem := pdk.AllocateBytes([]byte("{}"))
	resMem := pdk.FindMemory(users_getusers(mem.Offset()))
	var res usersResp
	if err := json.Unmarshal(resMem.ReadBytes(), &res); err != nil {
		return "admin"
	}
	for _, u := range res.Result {
		if u.IsAdmin {
			activeUsername = u.UserName
			pdk.Log(pdk.LogInfo, "Smart Playlist: Using authorized admin user: "+activeUsername)
			return activeUsername
		}
	}
	if len(res.Result) > 0 {
		activeUsername = res.Result[0].UserName
		pdk.Log(pdk.LogInfo, "Smart Playlist: Using authorized user: "+activeUsername)
		return activeUsername
	}
	return "admin"
}

func CallSubsonic(uri string) (string, error) {
	sep := "?"
	if strings.Contains(uri, "?") {
		sep = "&"
	}
	reqBytes, _ := json.Marshal(subsonicCallReq{Uri: fmt.Sprintf("%s%su=%s&f=json", uri, sep, GetActiveUsername())})
	mem := pdk.AllocateBytes(reqBytes)
	resMem := pdk.FindMemory(subsonicapi_call(mem.Offset()))
	var res subsonicCallResp
	if err := json.Unmarshal(resMem.ReadBytes(), &res); err != nil {
		return "", err
	}
	if res.Error != "" {
		return "", fmt.Errorf(res.Error)
	}
	return res.ResponseJSON, nil
}

func KvGet(key string) (string, bool) {
	reqBytes, _ := json.Marshal(kvGetReq{Key: key})
	mem := pdk.AllocateBytes(reqBytes)
	resMem := pdk.FindMemory(kvstore_get(mem.Offset()))
	var res kvGetResp
	if err := json.Unmarshal(resMem.ReadBytes(), &res); err != nil {
		return "", false
	}
	if res.Error != "" || !res.Exists {
		return "", false
	}
	return string(res.Value), true
}

func KvSet(key, value string) {
	reqBytes, _ := json.Marshal(kvSetReq{Key: key, Value: []byte(value)})
	mem := pdk.AllocateBytes(reqBytes)
	kvstore_set(mem.Offset())
}

func GetConfigString(key, defaultValue string) string {
	reqBytes, _ := json.Marshal(configReq{Key: key})
	mem := pdk.AllocateBytes(reqBytes)
	resMem := pdk.FindMemory(config_get(mem.Offset()))
	var res configStrResp
	json.Unmarshal(resMem.ReadBytes(), &res)
	if res.Exists {
		return res.Value
	}
	return defaultValue
}

func GetConfigInt(key string, defaultValue int) int {
	reqBytes, _ := json.Marshal(configReq{Key: key})
	mem := pdk.AllocateBytes(reqBytes)
	resMem := pdk.FindMemory(config_getint(mem.Offset()))
	var res configIntResp
	json.Unmarshal(resMem.ReadBytes(), &res)
	if res.Exists {
		return int(res.Value)
	}
	return defaultValue
}

func ScheduleRecurring(cronExpression, payload, scheduleID string) {
	req := struct {
		CronExpression string `json:"cronExpression"`
		Payload        string `json:"payload"`
		ScheduleID     string `json:"scheduleId"`
	}{cronExpression, payload, scheduleID}
	reqBytes, _ := json.Marshal(req)
	mem := pdk.AllocateBytes(reqBytes)
	scheduler_schedulerecurring(mem.Offset())
}
