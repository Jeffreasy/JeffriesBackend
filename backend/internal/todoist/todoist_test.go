package todoist

import "testing"

// TestTaskArgsSyncFormat locks in the exact Todoist Sync API arg shapes (due as
// {datetime,timezone}, duration as {amount,unit}) so the batch path can't drift
// from the documented format and corrupt real tasks.
func TestTaskArgsSyncFormat(t *testing.T) {
	c := NewClient("tok", "proj123")
	d := Dienst{
		EventID: "e1", Titel: "Dienst", StartDatum: "2026-06-22", StartTijd: "14:30",
		EindTijd: "22:00", Locatie: "Enckerkamp 31 Appartementen", ShiftType: "Laat",
		Duur: 7.5, Heledag: false,
	}
	args := c.taskArgs(d)
	if args["content"] != "R. Laat" {
		t.Fatalf("content = %v, want \"R. Laat\"", args["content"])
	}
	// Sync API: due uses the `date` field for a datetime (verified by live test).
	due, ok := args["due"].(map[string]any)
	if !ok || due["date"] != "2026-06-22T14:30:00" {
		t.Fatalf("due = %v", args["due"])
	}
	dur, ok := args["duration"].(map[string]any)
	if !ok || dur["amount"] != 450 || dur["unit"] != "minute" {
		t.Fatalf("duration = %v", args["duration"])
	}

	add := c.itemAdd(d)
	if add.Type != "item_add" || add.UUID == "" || add.TempID == "" || add.Args["project_id"] != "proj123" {
		t.Fatalf("item_add = %+v", add)
	}
	upd := c.itemUpdate("task9", d)
	if upd.Type != "item_update" || upd.Args["id"] != "task9" {
		t.Fatalf("item_update = %+v", upd)
	}
	if _, has := upd.Args["project_id"]; has {
		t.Fatal("item_update must not set project_id (would move the task)")
	}
	if cl := itemClose("task9"); cl.Type != "item_close" || cl.Args["id"] != "task9" {
		t.Fatalf("item_close = %+v", cl)
	}

	// All-day → due {date}, no duration.
	ad := c.taskArgs(Dienst{Locatie: "x", Heledag: true, StartDatum: "2026-06-22"})
	due2, _ := ad["due"].(map[string]any)
	if due2["date"] != "2026-06-22" {
		t.Fatalf("all-day due = %v", ad["due"])
	}
	if _, has := ad["duration"]; has {
		t.Fatal("all-day should have no duration")
	}
}
