package main

import "time"

// Keep the existing weekday pattern. Dates in the elapsed part of the first
// calendar week move to their first occurrence AFTER the selected start date.
func plannerScheduledDate(start time.Time, weekdayOffset int, window bool) string {
	if window {
		return start.Format("2006-01-02")
	}
	due := weekStart(start).AddDate(0, 0, weekdayOffset)
	if due.Before(start) {
		due = due.AddDate(0, 0, 7)
	}
	return due.Format("2006-01-02")
}

// All planner tasks store their chosen start in rotation_start, including plans
// without rotation. Legacy/manual tasks leave it blank and keep their behavior.
func plannerTaskHasStarted(start, day string) bool {
	return start == "" || (validDay(start) && day >= start)
}

func plannerDateDetails(p *PlannerPreview, start time.Time, people []string) {
	p.FirstWeekEnd = weekStart(start).AddDate(0, 0, 6).Format("2006-01-02")
	p.FirstWeekTotals = map[string]int{}
	p.FirstWeekMinutes = map[string]int{}
	for _, id := range people {
		p.FirstWeekTotals[id] = 0
		p.FirstWeekMinutes[id] = 0
	}
	for _, assignment := range p.Assignments {
		if assignment.DueDate >= p.StartDate && assignment.DueDate <= p.FirstWeekEnd {
			p.FirstWeekTotals[assignment.PersonID] += assignment.Points
			p.FirstWeekMinutes[assignment.PersonID] += assignment.Minutes
		}
	}
	if p.RotateWeeks > 0 && len(people) > 1 {
		p.FirstRotationDate = weekStart(start).AddDate(0, 0, 7*p.RotateWeeks).Format("2006-01-02")
	}
}
