package main

import (
 "net/http"
 "strings"
)

type CalendarOwner struct {
 EntityID string `json:"entity_id"`
 PersonID string `json:"person_id"`
}

// Stored per HA calendar entity, not per event; no Google write access needed.
func (a *App) calendarOwnerMap() (map[string]string,error) {
 rows,err:=a.db.Query("SELECT entity_id,person_id FROM calendar_owners")
 if err!=nil{return nil,err}
 owners:=map[string]string{}
 for _,row:=range rows {owners[row["entity_id"]]=row["person_id"]}
 return owners,nil
}

func applyCalendarOwners(events []Event,people []Person,owners map[string]string) []Event {
 out:=make([]Event,0,len(events))
 valid:=map[string]bool{}
 for _,person:=range people {valid[person.ID]=true}
 for _,event:=range events {
  event=decorateExternalEvent(event,people)
  // Explicit #Person tags always override the source calendar's default.
  if event.PersonID=="" {
   if owner:=owners[event.SourceCalendar];valid[owner] {
    event.PersonID=owner
    event.PersonIDs=[]string{owner}
   }
  }
  out=append(out,event)
 }
 return out
}

func (a *App) calendarOwnersGet(w http.ResponseWriter,r *http.Request) {
 owners,err:=a.calendarOwnerMap()
 if err!=nil{apiError(w,500,"Kalenderfarverne kunne ikke læses");return}
 valid:=map[string]bool{}
 people,err:=a.people()
 if err!=nil{apiError(w,500,"Familiemedlemmer kunne ikke læses");return}
 for _,p:=range people{valid[p.ID]=true}
 response:=[]CalendarOwner{}
 for _,entity:=range a.cfg.HACalendars {
  owner:=owners[entity]
  if !valid[owner] {owner=""}
  response=append(response,CalendarOwner{EntityID:entity,PersonID:owner})
 }
 writeJSON(w,200,response)
}

func (a *App) calendarOwnersPut(w http.ResponseWriter,r *http.Request) {
 var v CalendarOwner
 if !readJSON(w,r,&v){return}
 v.EntityID=strings.TrimSpace(v.EntityID)
 configured:=false
 for _,entity:=range a.cfg.HACalendars {if entity==v.EntityID {configured=true;break}}
 if !configured{apiError(w,400,"Kalenderen findes ikke i HA_CALENDARS");return}
 if v.PersonID!="" {
  people,err:=a.people()
  if err!=nil{apiError(w,500,"Familiemedlemmer kunne ikke læses");return}
  found:=false
  for _,person:=range people{if person.ID==v.PersonID{found=true;break}}
  if !found{apiError(w,400,"Den valgte ansvarlige findes ikke");return}
 }
 err:=a.db.Exec("INSERT INTO calendar_owners(entity_id,person_id) VALUES(?,?) ON CONFLICT(entity_id) DO UPDATE SET person_id=excluded.person_id",v.EntityID,v.PersonID)
 if err!=nil{apiError(w,500,"Kalenderejeren kunne ikke gemmes");return}
 a.broadcast()
 writeJSON(w,200,v)
}
