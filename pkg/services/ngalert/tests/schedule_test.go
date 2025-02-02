package tests

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/grafana/grafana/pkg/services/ngalert/eval"

	"github.com/grafana/grafana/pkg/services/ngalert/state"

	"github.com/grafana/grafana/pkg/infra/log"

	"github.com/grafana/grafana/pkg/services/ngalert/schedule"

	"github.com/grafana/grafana/pkg/registry"
	"github.com/grafana/grafana/pkg/services/ngalert/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/benbjohnson/clock"
)

type evalAppliedInfo struct {
	alertDefKey models.AlertDefinitionKey
	now         time.Time
}

func TestWarmStateCache(t *testing.T) {
	evaluationTime, _ := time.Parse("2006-01-02", "2021-03-25")

	expectedEntries := []state.AlertState{
		{
			UID:     "test_uid",
			OrgID:   123,
			CacheId: "test_uid test1=testValue1",
			Labels:  data.Labels{"test1": "testValue1"},
			State:   eval.Normal,
			Results: []state.StateEvaluation{
				{EvaluationTime: evaluationTime, EvaluationState: eval.Normal},
			},
			StartsAt:           evaluationTime.Add(-1 * time.Minute),
			EndsAt:             evaluationTime.Add(1 * time.Minute),
			LastEvaluationTime: evaluationTime,
		}, {
			UID:     "test_uid",
			OrgID:   123,
			CacheId: "test_uid test2=testValue2",
			Labels:  data.Labels{"test2": "testValue2"},
			State:   eval.Alerting,
			Results: []state.StateEvaluation{
				{EvaluationTime: evaluationTime, EvaluationState: eval.Alerting},
			},
			StartsAt:           evaluationTime.Add(-1 * time.Minute),
			EndsAt:             evaluationTime.Add(1 * time.Minute),
			LastEvaluationTime: evaluationTime,
		},
	}

	dbstore := setupTestEnv(t, 1)

	saveCmd1 := &models.SaveAlertInstanceCommand{
		DefinitionOrgID:   123,
		DefinitionUID:     "test_uid",
		Labels:            models.InstanceLabels{"test1": "testValue1"},
		State:             models.InstanceStateNormal,
		LastEvalTime:      evaluationTime,
		CurrentStateSince: evaluationTime.Add(-1 * time.Minute),
		CurrentStateEnd:   evaluationTime.Add(1 * time.Minute),
	}
	_ = dbstore.SaveAlertInstance(saveCmd1)

	saveCmd2 := &models.SaveAlertInstanceCommand{
		DefinitionOrgID:   123,
		DefinitionUID:     "test_uid",
		Labels:            models.InstanceLabels{"test2": "testValue2"},
		State:             models.InstanceStateFiring,
		LastEvalTime:      evaluationTime,
		CurrentStateSince: evaluationTime.Add(-1 * time.Minute),
		CurrentStateEnd:   evaluationTime.Add(1 * time.Minute),
	}
	_ = dbstore.SaveAlertInstance(saveCmd2)

	t.Cleanup(registry.ClearOverrides)

	schedCfg := schedule.SchedulerCfg{
		C:            clock.NewMock(),
		BaseInterval: time.Second,
		Logger:       log.New("ngalert cache warming test"),
		Store:        dbstore,
	}
	sched := schedule.NewScheduler(schedCfg, nil)
	st := state.NewStateTracker(schedCfg.Logger)
	sched.WarmStateCache(st)

	t.Run("instance cache has expected entries", func(t *testing.T) {
		for _, entry := range expectedEntries {
			cacheEntry := st.Get(entry.CacheId)
			assert.True(t, entry.Equals(cacheEntry))
		}
	})
}

func TestAlertingTicker(t *testing.T) {
	dbstore := setupTestEnv(t, 1)
	t.Cleanup(registry.ClearOverrides)

	alerts := make([]*models.AlertDefinition, 0)
	// create alert definition with zero interval (should never run)
	alerts = append(alerts, createTestAlertDefinition(t, dbstore, 0))

	// create alert definition with one second interval
	alerts = append(alerts, createTestAlertDefinition(t, dbstore, 1))

	evalAppliedCh := make(chan evalAppliedInfo, len(alerts))
	stopAppliedCh := make(chan models.AlertDefinitionKey, len(alerts))

	mockedClock := clock.NewMock()
	baseInterval := time.Second

	schedCfg := schedule.SchedulerCfg{
		C:            mockedClock,
		BaseInterval: baseInterval,
		EvalAppliedFunc: func(alertDefKey models.AlertDefinitionKey, now time.Time) {
			evalAppliedCh <- evalAppliedInfo{alertDefKey: alertDefKey, now: now}
		},
		StopAppliedFunc: func(alertDefKey models.AlertDefinitionKey) {
			stopAppliedCh <- alertDefKey
		},
		Store:  dbstore,
		Logger: log.New("ngalert schedule test"),
	}
	sched := schedule.NewScheduler(schedCfg, nil)

	ctx := context.Background()

	st := state.NewStateTracker(schedCfg.Logger)
	go func() {
		err := sched.Ticker(ctx, st)
		require.NoError(t, err)
	}()
	runtime.Gosched()

	expectedAlertDefinitionsEvaluated := []models.AlertDefinitionKey{alerts[1].GetKey()}
	t.Run(fmt.Sprintf("on 1st tick alert definitions: %s should be evaluated", concatenate(expectedAlertDefinitionsEvaluated)), func(t *testing.T) {
		tick := advanceClock(t, mockedClock)
		assertEvalRun(t, evalAppliedCh, tick, expectedAlertDefinitionsEvaluated...)
	})

	// change alert definition interval to three seconds
	var threeSecInterval int64 = 3
	err := dbstore.UpdateAlertDefinition(&models.UpdateAlertDefinitionCommand{
		UID:             alerts[0].UID,
		IntervalSeconds: &threeSecInterval,
		OrgID:           alerts[0].OrgID,
	})
	require.NoError(t, err)
	t.Logf("alert definition: %v interval reset to: %d", alerts[0].GetKey(), threeSecInterval)

	expectedAlertDefinitionsEvaluated = []models.AlertDefinitionKey{alerts[1].GetKey()}
	t.Run(fmt.Sprintf("on 2nd tick alert definition: %s should be evaluated", concatenate(expectedAlertDefinitionsEvaluated)), func(t *testing.T) {
		tick := advanceClock(t, mockedClock)
		assertEvalRun(t, evalAppliedCh, tick, expectedAlertDefinitionsEvaluated...)
	})

	expectedAlertDefinitionsEvaluated = []models.AlertDefinitionKey{alerts[1].GetKey(), alerts[0].GetKey()}
	t.Run(fmt.Sprintf("on 3rd tick alert definitions: %s should be evaluated", concatenate(expectedAlertDefinitionsEvaluated)), func(t *testing.T) {
		tick := advanceClock(t, mockedClock)
		assertEvalRun(t, evalAppliedCh, tick, expectedAlertDefinitionsEvaluated...)
	})

	expectedAlertDefinitionsEvaluated = []models.AlertDefinitionKey{alerts[1].GetKey()}
	t.Run(fmt.Sprintf("on 4th tick alert definitions: %s should be evaluated", concatenate(expectedAlertDefinitionsEvaluated)), func(t *testing.T) {
		tick := advanceClock(t, mockedClock)
		assertEvalRun(t, evalAppliedCh, tick, expectedAlertDefinitionsEvaluated...)
	})

	err = dbstore.DeleteAlertDefinitionByUID(&models.DeleteAlertDefinitionByUIDCommand{UID: alerts[1].UID, OrgID: alerts[1].OrgID})
	require.NoError(t, err)
	t.Logf("alert definition: %v deleted", alerts[1].GetKey())

	expectedAlertDefinitionsEvaluated = []models.AlertDefinitionKey{}
	t.Run(fmt.Sprintf("on 5th tick alert definitions: %s should be evaluated", concatenate(expectedAlertDefinitionsEvaluated)), func(t *testing.T) {
		tick := advanceClock(t, mockedClock)
		assertEvalRun(t, evalAppliedCh, tick, expectedAlertDefinitionsEvaluated...)
	})
	expectedAlertDefinitionsStopped := []models.AlertDefinitionKey{alerts[1].GetKey()}
	t.Run(fmt.Sprintf("on 5th tick alert definitions: %s should be stopped", concatenate(expectedAlertDefinitionsStopped)), func(t *testing.T) {
		assertStopRun(t, stopAppliedCh, expectedAlertDefinitionsStopped...)
	})

	expectedAlertDefinitionsEvaluated = []models.AlertDefinitionKey{alerts[0].GetKey()}
	t.Run(fmt.Sprintf("on 6th tick alert definitions: %s should be evaluated", concatenate(expectedAlertDefinitionsEvaluated)), func(t *testing.T) {
		tick := advanceClock(t, mockedClock)
		assertEvalRun(t, evalAppliedCh, tick, expectedAlertDefinitionsEvaluated...)
	})

	// create alert definition with one second interval
	alerts = append(alerts, createTestAlertDefinition(t, dbstore, 1))

	expectedAlertDefinitionsEvaluated = []models.AlertDefinitionKey{alerts[2].GetKey()}
	t.Run(fmt.Sprintf("on 7th tick alert definitions: %s should be evaluated", concatenate(expectedAlertDefinitionsEvaluated)), func(t *testing.T) {
		tick := advanceClock(t, mockedClock)
		assertEvalRun(t, evalAppliedCh, tick, expectedAlertDefinitionsEvaluated...)
	})

	// pause alert definition
	err = dbstore.UpdateAlertDefinitionPaused(&models.UpdateAlertDefinitionPausedCommand{UIDs: []string{alerts[2].UID}, OrgID: alerts[2].OrgID, Paused: true})
	require.NoError(t, err)
	t.Logf("alert definition: %v paused", alerts[2].GetKey())

	expectedAlertDefinitionsEvaluated = []models.AlertDefinitionKey{}
	t.Run(fmt.Sprintf("on 8th tick alert definitions: %s should be evaluated", concatenate(expectedAlertDefinitionsEvaluated)), func(t *testing.T) {
		tick := advanceClock(t, mockedClock)
		assertEvalRun(t, evalAppliedCh, tick, expectedAlertDefinitionsEvaluated...)
	})

	expectedAlertDefinitionsStopped = []models.AlertDefinitionKey{alerts[2].GetKey()}
	t.Run(fmt.Sprintf("on 8th tick alert definitions: %s should be stopped", concatenate(expectedAlertDefinitionsStopped)), func(t *testing.T) {
		assertStopRun(t, stopAppliedCh, expectedAlertDefinitionsStopped...)
	})

	// unpause alert definition
	err = dbstore.UpdateAlertDefinitionPaused(&models.UpdateAlertDefinitionPausedCommand{UIDs: []string{alerts[2].UID}, OrgID: alerts[2].OrgID, Paused: false})
	require.NoError(t, err)
	t.Logf("alert definition: %v unpaused", alerts[2].GetKey())

	expectedAlertDefinitionsEvaluated = []models.AlertDefinitionKey{alerts[0].GetKey(), alerts[2].GetKey()}
	t.Run(fmt.Sprintf("on 9th tick alert definitions: %s should be evaluated", concatenate(expectedAlertDefinitionsEvaluated)), func(t *testing.T) {
		tick := advanceClock(t, mockedClock)
		assertEvalRun(t, evalAppliedCh, tick, expectedAlertDefinitionsEvaluated...)
	})
}

func assertEvalRun(t *testing.T, ch <-chan evalAppliedInfo, tick time.Time, keys ...models.AlertDefinitionKey) {
	timeout := time.After(time.Second)

	expected := make(map[models.AlertDefinitionKey]struct{}, len(keys))
	for _, k := range keys {
		expected[k] = struct{}{}
	}

	for {
		select {
		case info := <-ch:
			_, ok := expected[info.alertDefKey]
			t.Logf("alert definition: %v evaluated at: %v", info.alertDefKey, info.now)
			assert.True(t, ok)
			assert.Equal(t, tick, info.now)
			delete(expected, info.alertDefKey)
			if len(expected) == 0 {
				return
			}
		case <-timeout:
			if len(expected) == 0 {
				return
			}
			t.Fatal("cycle has expired")
		}
	}
}

func assertStopRun(t *testing.T, ch <-chan models.AlertDefinitionKey, keys ...models.AlertDefinitionKey) {
	timeout := time.After(time.Second)

	expected := make(map[models.AlertDefinitionKey]struct{}, len(keys))
	for _, k := range keys {
		expected[k] = struct{}{}
	}

	for {
		select {
		case alertDefKey := <-ch:
			_, ok := expected[alertDefKey]
			t.Logf("alert definition: %v stopped", alertDefKey)
			assert.True(t, ok)
			delete(expected, alertDefKey)
			if len(expected) == 0 {
				return
			}
		case <-timeout:
			if len(expected) == 0 {
				return
			}
			t.Fatal("cycle has expired")
		}
	}
}

func advanceClock(t *testing.T, mockedClock *clock.Mock) time.Time {
	mockedClock.Add(time.Second)
	return mockedClock.Now()
	// t.Logf("Tick: %v", mockedClock.Now())
}

func concatenate(keys []models.AlertDefinitionKey) string {
	s := make([]string, len(keys))
	for _, k := range keys {
		s = append(s, k.String())
	}
	return fmt.Sprintf("[%s]", strings.Join(s, ","))
}
