// Package bucket reads and writes the data of a charging pool in its TagoIO device bucket, by pool code: it
// resolves the pool's device token, recovers from a full bucket and shows the dashboard validation toasts.
//
// A TagoIO device stores at most 50 000 records; past that every insert fails, so the status and session
// variables the service writes all the time are trimmed regularly (CleanupPool, and the maintenance jobs).
package bucket

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/devices"
	"github.com/dgarciacampelo/tagoio-handler/internal/tagoio"
)

// Retention of the variables trimmed by CleanupPool.
const routineRetentionWeeks = 2

const (
	replaceSettle  = time.Second      // Pause between deleting and re-inserting a variable (TagoIO applies deletes asynchronously)
	feedbackLength = 10 * time.Second // How long a validation toast stays before it is removed
)

// Connector is a known station connector of a pool.
type Connector struct {
	StationName string
	ConnectorID int
}

// ConnectorLister lists the known connectors of a pool (their public dashboard variables are trimmed too).
type ConnectorLister interface {
	PoolConnectors(poolCode int) []Connector
}

// Service accesses the pools' buckets.
type Service struct {
	tago       *tagoio.Client
	devices    *devices.Registry
	connectors ConnectorLister
	log        *slog.Logger
}

// New creates the bucket service.
func New(tago *tagoio.Client, registry *devices.Registry, connectors ConnectorLister, log *slog.Logger) *Service {
	return &Service{tago: tago, devices: registry, connectors: connectors, log: log}
}

func (s *Service) token(ctx context.Context, poolCode int) (string, error) {
	d, err := s.devices.ByPool(ctx, poolCode)
	if err != nil {
		return "", err
	}
	return d.Token, nil
}

// Insert adds records to a pool's bucket. When the bucket is full, it trims the pool's routine variables and
// retries once.
func (s *Service) Insert(ctx context.Context, poolCode int, records ...tagoio.DataInput) error {
	token, err := s.token(ctx, poolCode)
	if err != nil {
		return err
	}
	err = s.tago.InsertData(ctx, token, records...)
	if tagoio.HasMessage(err, tagoio.DeviceFullMessage) {
		s.log.Warn("Bucket: device full, trimming and retrying", "pool_code", poolCode)
		s.CleanupPool(ctx, poolCode)
		err = s.tago.InsertData(ctx, token, records...)
	}
	if err != nil {
		return fmt.Errorf("bucket: inserting into pool %d: %w", poolCode, err)
	}
	return nil
}

// Delete removes the records of a variable (only those of group, when set) older than keepWeeks weeks; 0 removes
// them all. It returns how many records TagoIO removed.
func (s *Service) Delete(ctx context.Context, poolCode int, variable, group string, keepWeeks int) (int, error) {
	token, err := s.token(ctx, poolCode)
	if err != nil {
		return 0, err
	}
	filter := tagoio.DeleteFilter{Variable: variable, Group: group}
	if keepWeeks > 0 {
		filter.Before = time.Now().AddDate(0, 0, -7*keepWeeks)
	}
	count, err := s.tago.DeleteData(ctx, token, filter)
	if err != nil {
		return 0, fmt.Errorf("bucket: deleting %s from pool %d: %w", variable, poolCode, err)
	}
	return count, nil
}

// Replace deletes every record of a variable (of group, when set) and inserts record, so the dashboards show
// only the new value.
func (s *Service) Replace(ctx context.Context, poolCode int, variable, group string, record tagoio.DataInput) error {
	if _, err := s.Delete(ctx, poolCode, variable, group, 0); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(replaceSettle):
	}
	return s.Insert(ctx, poolCode, record)
}

// LastValue returns the latest record of a variable in a pool's bucket, or nil.
func (s *Service) LastValue(ctx context.Context, poolCode int, variable string) (*tagoio.DataRecord, error) {
	token, err := s.token(ctx, poolCode)
	if err != nil {
		return nil, err
	}
	record, err := s.tago.LastValue(ctx, token, variable)
	if err != nil {
		return nil, fmt.Errorf("bucket: reading %s of pool %d: %w", variable, poolCode, err)
	}
	return record, nil
}

// List returns up to qty latest records of a variable in a pool's bucket.
func (s *Service) List(ctx context.Context, poolCode int, variable string, qty int) ([]tagoio.DataRecord, error) {
	token, err := s.token(ctx, poolCode)
	if err != nil {
		return nil, err
	}
	records, err := s.tago.GetData(ctx, token, variable, qty, 0)
	if err != nil {
		return nil, fmt.Errorf("bucket: listing %s of pool %d: %w", variable, poolCode, err)
	}
	return records, nil
}

// Validation toast types understood by the TagoIO dashboard forms.
const (
	feedbackAccept = "accept"
	feedbackDanger = "danger"
)

// Feedback shows a validation toast ("OK" or "ERROR") in a pool's dashboard: it inserts the toast variable and
// removes it again after a few seconds. It returns at once; the removal runs in the background.
func (s *Service) Feedback(poolCode int, variable string, ok bool) {
	message, kind := "OK", feedbackAccept
	if !ok {
		message, kind = "ERROR", feedbackDanger
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		toast := tagoio.DataInput{Variable: variable, Value: message, Group: "validation_feedback", Metadata: map[string]any{"type": kind}}
		if err := s.Insert(ctx, poolCode, toast); err != nil {
			s.log.Error("Bucket: validation toast failed", "pool_code", poolCode, "variable", variable, "error", err)
			return
		}
		time.Sleep(feedbackLength)
		if _, err := s.Delete(ctx, poolCode, variable, "", 0); err != nil {
			s.log.Error("Bucket: removing validation toast failed", "pool_code", poolCode, "variable", variable, "error", err)
		}
	}()
}

// CleanupPool trims the routine variables of a pool older than two weeks: the active session records, the
// management dashboard states and the public dashboard variables of every known connector. It returns how many
// records were removed.
func (s *Service) CleanupPool(ctx context.Context, poolCode int) int {
	variables := []string{"active_cs_data", "state"}
	for _, c := range s.connectors.PoolConnectors(poolCode) {
		for _, prefix := range []string{"cost", "energy", "state", "time"} {
			variables = append(variables, fmt.Sprintf("%s_%s_%d", prefix, c.StationName, c.ConnectorID))
		}
	}

	removed := 0
	for _, variable := range variables {
		count, err := s.Delete(ctx, poolCode, variable, "", routineRetentionWeeks)
		if err != nil {
			s.log.Warn("Bucket: cleanup delete failed", "pool_code", poolCode, "variable", variable, "error", err)
			continue
		}
		removed += count
	}
	s.log.Info("Bucket: pool cleanup done", "pool_code", poolCode, "removed", removed)
	return removed
}
