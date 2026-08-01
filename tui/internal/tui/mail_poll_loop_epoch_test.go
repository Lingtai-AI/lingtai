package tui

import "testing"

func newPollEpochTestApp(t *testing.T) App {
	t.Helper()
	globalDir := t.TempDir()
	app := App{
		currentView: appViewHelp,
		globalDir:   globalDir,
	}
	app.installMailModel(NewMailModel(
		t.TempDir(),
		"human",
		t.TempDir(),
		"",
		"",
		200,
		globalDir,
		"en",
		false,
		0,
	))
	return app
}

func TestMailPollLoopEpochRejectsOldTickAfterSameGenerationReturn(t *testing.T) {
	app := newPollEpochTestApp(t)
	oldTick := tickMsg{
		generation: app.mail.generation,
		pollEpoch:  app.mail.pollEpoch,
	}

	returnedModel, _ := app.Update(MarkdownViewerCloseMsg{})
	returned := returnedModel.(App)
	if returned.mail.generation != oldTick.generation {
		t.Fatalf("same-generation return changed Mail generation from %d to %d", oldTick.generation, returned.mail.generation)
	}
	if returned.mail.pollEpoch == oldTick.pollEpoch {
		t.Fatalf("same-generation return kept stale poll epoch %d", oldTick.pollEpoch)
	}
	serialAfterReturn := returned.mail.refreshRequestSerial

	updatedModel, cmd := returned.Update(oldTick)
	updated := updatedModel.(App)
	if updated.mail.refreshRequestSerial != serialAfterReturn {
		t.Fatalf(
			"old tick advanced refresh serial after same-generation return: got %d, want %d",
			updated.mail.refreshRequestSerial,
			serialAfterReturn,
		)
	}
	if cmd != nil {
		t.Fatal("old tick returned a refresh/rearm command after same-generation return")
	}

	liveEpoch := returned.mail.pollEpoch
	updatedModel, cmd = returned.Update(tickMsg{
		generation: returned.mail.generation,
		pollEpoch:  liveEpoch,
	})
	updated = updatedModel.(App)
	if updated.mail.refreshRequestSerial != serialAfterReturn+1 {
		t.Fatalf(
			"live tick advanced refresh serial by %d, want exactly 1",
			updated.mail.refreshRequestSerial-serialAfterReturn,
		)
	}
	if cmd == nil {
		t.Fatal("live tick did not return its refresh/rearm command")
	}
	if updated.mail.pollEpoch != liveEpoch {
		t.Fatalf("live tick changed poll epoch from %d to %d", liveEpoch, updated.mail.pollEpoch)
	}
}

func TestMailPollLoopEpochRejectsOldPulseAfterSameGenerationReturn(t *testing.T) {
	app := newPollEpochTestApp(t)
	app.mail.orchState = "ACTIVE"
	oldPulse := pulseTickMsg{
		generation: app.mail.generation,
		pollEpoch:  app.mail.pollEpoch,
	}

	returnedModel, _ := app.Update(MarkdownViewerCloseMsg{})
	returned := returnedModel.(App)
	pulseBefore := returned.mail.pulseTick

	updatedModel, cmd := returned.Update(oldPulse)
	updated := updatedModel.(App)
	if updated.mail.pulseTick != pulseBefore {
		t.Fatalf("old pulse advanced animation from %d to %d", pulseBefore, updated.mail.pulseTick)
	}
	if cmd != nil {
		t.Fatal("old pulse returned a rearm command after same-generation return")
	}

	liveEpoch := returned.mail.pollEpoch
	updatedModel, cmd = returned.Update(pulseTickMsg{
		generation: returned.mail.generation,
		pollEpoch:  liveEpoch,
	})
	updated = updatedModel.(App)
	if updated.mail.pulseTick != pulseBefore+1 {
		t.Fatalf("live pulse advanced animation by %d, want exactly 1", updated.mail.pulseTick-pulseBefore)
	}
	if cmd == nil {
		t.Fatal("live pulse did not return its rearm command")
	}
	if updated.mail.pollEpoch != liveEpoch {
		t.Fatalf("live pulse changed poll epoch from %d to %d", liveEpoch, updated.mail.pollEpoch)
	}
}

func TestMailPollLoopEpochAdvancesAtEveryLoopStart(t *testing.T) {
	app := newPollEpochTestApp(t)
	if app.mail.pollEpoch == 0 {
		t.Fatal("initial Mail installation did not establish a poll epoch")
	}

	beforeSwitch := app.mail.pollEpoch
	switchedModel, _ := app.switchToView("mail")
	switched := switchedModel.(App)
	if switched.mail.generation != app.mail.generation {
		t.Fatalf("ordinary Mail return changed generation from %d to %d", app.mail.generation, switched.mail.generation)
	}
	if switched.mail.pollEpoch == beforeSwitch {
		t.Fatalf("ordinary Mail return kept poll epoch %d", beforeSwitch)
	}

	beforeResumeGeneration := switched.mail.generation
	beforeResumeEpoch := switched.mail.pollEpoch
	resumeCmd := switched.resumeMailModel(switched.mail)
	if resumeCmd == nil {
		t.Fatal("restored Mail model did not schedule refresh and poll commands")
	}
	if switched.mail.generation == beforeResumeGeneration {
		t.Fatalf("restored Mail model kept generation %d", beforeResumeGeneration)
	}
	if switched.mail.pollEpoch == beforeResumeEpoch {
		t.Fatalf("restored Mail model kept poll epoch %d", beforeResumeEpoch)
	}
}
