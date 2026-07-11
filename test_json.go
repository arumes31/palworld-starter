package main

import (
	"encoding/json"
	"fmt"
)

type PlayerInfo struct {
	Name  string `json:"name"`
	Level int    `json:"level"`
}

func main() {
	body := `{"players": [{"name": "cari", "level": 50.0}]}`
	var r struct {
		Players []PlayerInfo `json:"players"`
	}
	err := json.Unmarshal([]byte(body), &r)
	fmt.Println("Float level error:", err)

	body2 := `{"players": [{"name": "cari", "level": "50"}]}`
	err2 = json.Unmarshal([]byte(body2), &r)
	fmt.Println("String level error:", err2)
}
