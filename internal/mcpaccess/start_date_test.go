package mcpaccess

import "testing"

func TestShiftPlanPreservesMidweekStart(t *testing.T) {
	f := setup(t)
	seed(t, f)
	if err := f.db.Exec("UPDATE tasks SET due_date=date(due_date,'+3 days'),rotation_start='2026-11-05' WHERE plan_id=?", planID); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.previewShift("admin", shiftRequest{PlanID: planID, NewStart: "2026-10-26"})
	if err != nil {
		t.Fatal(err)
	}
	p := got.(proposal)
	if p.DeltaDays != -7 {
		t.Fatalf("expected a whole-week move: %+v", p)
	}
	for _, c := range p.Changes {
		if c.OldRotation != "2026-11-05" || c.NewRotation != "2026-10-29" {
			t.Fatalf("start weekday not preserved: %+v", c)
		}
	}
	if _, err := f.s.applyShift("admin", applyRequest{ID: p.ID, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	rows, err := planRows(f.db, planID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r["rotation_start"] != "2026-10-29" {
			t.Fatalf("wrong saved start: %v", r)
		}
	}
}
