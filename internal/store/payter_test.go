package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// terminalsBySerial returns the terminals by serial number, without their update time.
func terminalsBySerial(t *testing.T, s *Store) map[string]PayterTerminal {
	t.Helper()
	terminals, err := s.PayterTerminals(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]PayterTerminal{}
	for _, terminal := range terminals {
		terminal.UpdatedAt = ""
		result[terminal.SerialNumber] = terminal
	}
	return result
}

func TestANewStoreHasTheProductionPayterTerminals(t *testing.T) {
	s := openTestStore(t)

	want := map[string]PayterTerminal{
		"P6X20222100772": {SerialNumber: "P6X20222100772", PoolCode: 221006, LinkedStations: []string{"22V20538CCPOS"}, IsActive: true},
		"P6X20222101349": {SerialNumber: "P6X20222101349", PoolCode: 221006, LinkedStations: []string{"22V20537CCPOS"}, IsActive: true},
		"P6X20231300653": {SerialNumber: "P6X20231300653", PoolCode: 241004, IsPoolShared: true, LinkedStations: []string{}, IsActive: true},
	}
	if got := terminalsBySerial(t, s); !reflect.DeepEqual(got, want) {
		t.Fatalf("terminals = %+v\nwant %+v", got, want)
	}
}

func TestSavingATerminalMovesItsStationsFromOtherTerminals(t *testing.T) {
	s := openTestStore(t)

	moved, err := s.SavePayterTerminal(context.Background(), PayterTerminal{
		SerialNumber: "P6X20250000001", PoolCode: 221006, LinkedStations: []string{"22V20537CCPOS", "22V20539CCPOS"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if want := map[string]string{"22V20537CCPOS": "P6X20222101349"}; !reflect.DeepEqual(moved, want) {
		t.Errorf("moved = %v; want %v", moved, want)
	}
	terminals := terminalsBySerial(t, s)
	if got := terminals["P6X20250000001"].LinkedStations; !reflect.DeepEqual(got, []string{"22V20537CCPOS", "22V20539CCPOS"}) {
		t.Errorf("new terminal stations = %v", got)
	}
	if got := terminals["P6X20222101349"].LinkedStations; len(got) != 0 {
		t.Errorf("previous terminal stations = %v; want none", got)
	}
}

func TestASharedTerminalHasNoStationsAndIsTheOnlyOneOfItsPool(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.SavePayterTerminal(ctx, PayterTerminal{SerialNumber: "P6X20222101349", PoolCode: 221006, IsPoolShared: true,
		LinkedStations: []string{"22V20537CCPOS"}}); err != nil {
		t.Fatal(err)
	}
	if got := terminalsBySerial(t, s)["P6X20222101349"]; !got.IsPoolShared || len(got.LinkedStations) != 0 {
		t.Errorf("shared terminal = %+v; want shared, without stations", got)
	}

	_, err := s.SavePayterTerminal(ctx, PayterTerminal{SerialNumber: "P6X20250000001", PoolCode: 241004, IsPoolShared: true})
	if !errors.Is(err, ErrSharedTerminalExists) {
		t.Fatalf("second shared terminal of pool 241004: err = %v; want ErrSharedTerminalExists", err)
	}
}

func TestADeactivatedTerminalIsKeptWithoutStationsAndCanBeReactivated(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	poolCode, found, err := s.DeactivatePayterTerminal(ctx, "P6X20222101349")
	if err != nil || !found || poolCode != 221006 {
		t.Fatalf("Deactivate = %d, %v, %v; want 221006, true, nil", poolCode, found, err)
	}
	if got := terminalsBySerial(t, s)["P6X20222101349"]; got.IsActive || len(got.LinkedStations) != 0 {
		t.Errorf("deactivated terminal = %+v; want inactive, without stations", got)
	}

	if _, found, _ := s.DeactivatePayterTerminal(ctx, "P6X99999999999"); found {
		t.Error("an unknown terminal was found")
	}

	if _, err := s.SavePayterTerminal(ctx, PayterTerminal{SerialNumber: "P6X20222101349", PoolCode: 221006,
		LinkedStations: []string{"22V20537CCPOS"}}); err != nil {
		t.Fatal(err)
	}
	if got := terminalsBySerial(t, s)["P6X20222101349"]; !got.IsActive {
		t.Errorf("saved again = %+v; want active", got)
	}
}
