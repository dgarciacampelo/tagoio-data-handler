// This file holds the contract of the Payter terminals page of the eMSP dashboard.

package dto

// PayterPage is the view model of the Payter terminals page.
type PayterPage struct {
	Terminals []PayterTerminal `json:"terminals"`
	Pools     []PayterPool     `json:"pools"` // The pools of the terminals and of the stations that reported
}

// PayterPool is a pool a terminal can be set up in, with the stations the service knows (station picker).
type PayterPool struct {
	PoolCode int      `json:"pool_code"`
	CPOName  string   `json:"cpo_name"`
	Stations []string `json:"stations"`
}

// PayterTerminal is a Payter terminal and the stations it serves.
//
// ! Wire contract: neos-ocpp reads the list from GET /api/payter-terminals (PayterTerminalSetup there) at startup.
type PayterTerminal struct {
	SerialNumber   string   `json:"serial_number"`
	PoolCode       int      `json:"pool_code"`
	IsPoolShared   bool     `json:"is_pool_shared"`  // Serves every station of the pool
	LinkedStations []string `json:"linked_stations"` // Empty when IsPoolShared
	IsActive       bool     `json:"is_active"`       // A deactivated terminal is kept: the CSMS keeps its payments
	UpdatedAt      string   `json:"updated_at"`      // UTC, "2006-01-02 15:04:05"
}

// PayterTerminalInput is the body of PUT /api/admin/payter-terminals/{serial_number}, which answers with every
// terminal ([]PayterTerminal), as DELETE does.
type PayterTerminalInput struct {
	PoolCode       int      `json:"pool_code"`
	IsPoolShared   bool     `json:"is_pool_shared"`
	LinkedStations []string `json:"linked_stations"`
}
