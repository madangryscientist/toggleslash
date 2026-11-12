package toggleslash

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/madangryscientist/toggleslash/sdks/go/internal/jsonx"
)

// usageBodies is every usage request body the fake server saw, parsed.
func usageBodies(t *testing.T, server *fakeServer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, request := range server.seenRequests() {
		if request.path != "/sdk/v1/events" {
			continue
		}
		value, err := jsonx.Parse(request.body)
		if err != nil {
			t.Fatalf("parsing a usage body: %v", err)
		}
		record, _ := value.(map[string]any)
		out = append(out, record)
	}
	return out
}

// usageEntries is the events of one usage body.
func usageEntries(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	events, ok := body["events"].([]any)
	if !ok {
		t.Fatalf("the usage body holds %v", body)
	}
	out := make([]map[string]any, 0, len(events))
	for _, raw := range events {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("a usage entry is %v", raw)
		}
		if len(entry) != 4 {
			t.Errorf("a usage entry has %d fields: %v", len(entry), entry)
		}
		out = append(out, entry)
	}
	return out
}

func TestCountsAreSummedPerFlagVariationAndDayAndSentOnClose(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	client := start(t, server, nil)
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	for i := 0; i < 3; i++ {
		_ = client.BoolVariation("checkout", Context{"key": "u"}, false)
	}
	_ = client.Variation("missing", Context{"key": "u"}, nil)
	_ = client.BoolVariation("checkout", Context{}, false)
	client.Close()
	bodies := usageBodies(t, server)
	if len(bodies) != 1 {
		t.Fatalf("%d usage requests went out, expected one", len(bodies))
	}
	entries := usageEntries(t, bodies[0])
	if len(entries) != 1 {
		t.Fatalf("%d entries went out, expected one", len(entries))
	}
	entry := entries[0]
	if entry["flag"] != "checkout" || entry["variationIndex"] != 1.0 || entry["count"] != 3.0 {
		t.Errorf("the entry was %v", entry)
	}
	if day, ok := entry["day"].(string); !ok || len(day) != 10 || day[4] != '-' {
		t.Errorf("the day was %v", entry["day"])
	}
}

func TestAFlushWithNothingToSendSendsNothing(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	client := start(t, server, func(cfg *config) { cfg.flushInterval = 10 * time.Millisecond })
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	time.Sleep(60 * time.Millisecond)
	client.Close()
	if got := server.countFor("/sdk/v1/events"); got != 0 {
		t.Errorf("%d empty flushes went out", got)
	}
}

func TestAFlushOfMoreThanFiveHundredEntriesIsSeveralRequests(t *testing.T) {
	var flags []string
	for i := 0; i < 620; i++ {
		flags = append(flags, flagJSON(fmt.Sprintf("flag-%03d", i), true, 1))
	}
	server := newFakeServer(t, strings.Join(flags, ","))
	client := start(t, server, nil)
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	for i := 0; i < 620; i++ {
		_ = client.BoolVariation(fmt.Sprintf("flag-%03d", i), Context{"key": "u"}, false)
	}
	client.Close()
	bodies := usageBodies(t, server)
	if len(bodies) != 2 {
		t.Fatalf("%d usage requests went out, expected two", len(bodies))
	}
	if got := len(usageEntries(t, bodies[0])); got != 500 {
		t.Errorf("the first batch held %d entries, expected 500", got)
	}
	if got := len(usageEntries(t, bodies[1])); got != 120 {
		t.Errorf("the second batch held %d entries, expected 120", got)
	}
}

func TestCountsAreKeptForARetryableAnswerAndDroppedForAnother(t *testing.T) {
	for _, one := range []struct {
		status int
		kept   bool
	}{
		{500, true}, {408, true}, {429, true}, {400, false}, {401, false}, {413, false},
	} {
		t.Run(fmt.Sprintf("%d", one.status), func(t *testing.T) {
			server := newFakeServer(t, flagJSON("checkout", true, 1))
			server.set(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/sdk/v1/events" {
					server.defaultAnswer(writer, request)
					return
				}
				writer.WriteHeader(one.status)
			})
			client := start(t, server, nil)
			if !client.Ready() {
				t.Fatal("the first load failed")
			}
			_ = client.BoolVariation("checkout", Context{"key": "u"}, false)
			client.Close()
			if kept := client.usage.pendingCount() > 0; kept != one.kept {
				t.Errorf("pending after a %d was %v, expected kept=%v", one.status, kept, one.kept)
			}
		})
	}
}

func TestCountsEitherSideOfAUTCMidnightAreTwoDays(t *testing.T) {
	moment := time.Date(2026, 11, 10, 23, 59, 59, 0, time.UTC)
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	client := start(t, server, func(cfg *config) {
		cfg.now = func() time.Time { return moment }
	})
	defer client.Close()
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	_ = client.BoolVariation("checkout", Context{"key": "u"}, false)
	moment = moment.Add(2 * time.Second)
	_ = client.BoolVariation("checkout", Context{"key": "u"}, false)
	entries := client.usage.take()
	if len(entries) != 2 {
		t.Fatalf("%d entries were recorded, expected two days", len(entries))
	}
	days := map[string]bool{}
	for _, entry := range entries {
		days[entry.key.day] = true
	}
	if !days["2026-11-10"] || !days["2026-11-11"] {
		t.Errorf("the days were %v", days)
	}
}

func TestAFlushDuringAHoldSendsNothing(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	client := start(t, server, nil)
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	_ = client.BoolVariation("checkout", Context{"key": "u"}, false)
	client.usage.hold.raiseTo(5 * time.Second)
	client.usage.flush()
	if got := server.countFor("/sdk/v1/events"); got != 0 {
		t.Errorf("%d usage requests went out during a hold", got)
	}
	if client.usage.pendingCount() != 1 {
		t.Error("a flush during a hold lost the counts")
	}
}

func TestCloseDuringAHoldSaysTheCountsWereLost(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	var reported recorder
	client := start(t, server, func(cfg *config) {
		cfg.onError = reported.add
	})
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	_ = client.BoolVariation("checkout", Context{"key": "u"}, false)
	_ = client.BoolVariation("checkout", Context{"key": "u"}, false)
	client.usage.hold.raiseTo(30 * time.Second)
	client.Close()
	found := false
	for _, message := range reported.messages() {
		if strings.Contains(message, "1 usage counts unsent") && strings.Contains(message, "asked to wait") {
			found = true
		}
	}
	if !found {
		t.Errorf("the lost counts were not reported: %v", reported.messages())
	}
	if got := server.countFor("/sdk/v1/events"); got != 0 {
		t.Errorf("%d usage requests went out during the hold", got)
	}
}

func TestAUsageRequestIsBoundedByTheRequestTimeout(t *testing.T) {
	server := newFakeServer(t, flagJSON("checkout", true, 1))
	server.set(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/sdk/v1/events" {
			server.defaultAnswer(writer, request)
			return
		}
		<-request.Context().Done()
	})
	client := start(t, server, func(cfg *config) { cfg.requestTimeout = 150 * time.Millisecond })
	if !client.Ready() {
		t.Fatal("the first load failed")
	}
	_ = client.BoolVariation("checkout", Context{"key": "u"}, false)
	started := time.Now()
	client.Close()
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("close took %v with a hanging flush", elapsed)
	}
	if client.usage.pendingCount() != 1 {
		t.Error("a timed-out usage request did not keep its counts")
	}
}
