package engine_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

func TestSettingsStartAtDefaultsAndSurviveRestart(t *testing.T) {
	c, dir := enginetest.Start(t)
	s, err := c.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if s != (engine.Settings{CardCorner: "top-right", PingSound: "first", VoiceModel: "none"}) {
		t.Fatalf("fresh settings %+v", s)
	}
	corner, done, sound, on, follows := "bottom-left", true, "first-and-follow-up", true, true
	vm, url, name := "endpoint", "http://localhost:11434/v1", "llama3.2"
	got, err := c.ChangeSettings(engine.ChangeSettings{CardCorner: &corner, CardFollowsActiveDisplay: &follows, FirstLaunchDone: &done, PingSound: &sound, Voice: &on, VoiceAutoListen: &on,
		VoiceModel: &vm, VoiceEndpoint: &url, VoiceEndpointModel: &name})
	if err != nil {
		t.Fatal(err)
	}
	want := engine.Settings{CardCorner: "bottom-left", CardFollowsActiveDisplay: true, FirstLaunchDone: true, PingSound: "first-and-follow-up", Voice: true, VoiceAutoListen: true,
		VoiceModel: "endpoint", VoiceEndpoint: "http://localhost:11434/v1", VoiceEndpointModel: "llama3.2"}
	if got != want {
		t.Fatalf("changed settings %+v", got)
	}
	// Absent parts stay.
	told := true
	if got, err = c.ChangeSettings(engine.ChangeSettings{ToldLoginDisallowed: &told}); err != nil {
		t.Fatal(err)
	}
	want.ToldLoginDisallowed = true
	if got != want {
		t.Fatalf("after second change %+v", got)
	}
	c = enginetest.Restart(t, dir)
	if s, _ := c.Settings(); s != want {
		t.Fatalf("after restart %+v", s)
	}
}

func TestSettingsRefuseUnknownValues(t *testing.T) {
	c, _ := enginetest.Start(t)
	bad, notURL := "middle", "localhost:11434"
	for _, ch := range []engine.ChangeSettings{{CardCorner: &bad}, {PingSound: &bad}, {VoiceModel: &bad}, {VoiceEndpoint: &notURL}} {
		_, err := c.ChangeSettings(ch)
		var e *client.Error
		if !errors.As(err, &e) || e.Status != 400 || e.Message == "" {
			t.Fatalf("want 400 with a message, got %v", err)
		}
	}
}

func TestChangingSettingsIsAnEvent(t *testing.T) {
	c, _ := enginetest.Start(t)
	ev, stop, err := c.Events(-1)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	corner := "top-left"
	if _, err := c.ChangeSettings(engine.ChangeSettings{CardCorner: &corner}); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-ev:
			var s engine.Settings
			if e.Kind == engine.EventSettingsChanged && jsonInto(e.Data, &s) && s.CardCorner == corner {
				return
			}
		case <-deadline:
			t.Fatal("no settings.changed event")
		}
	}
}
