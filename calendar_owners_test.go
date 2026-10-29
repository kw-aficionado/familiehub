package main

import (
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"
 "strings"
 "fmt"
)

func TestCalendarOwnersPersonTagsOverrideDefaults(t *testing.T){
 people:=[]Person{{ID:"anton123",Name:"Anton",Color:"#d59c19"},{ID:"alma123",Name:"Alma",Color:"#d35450"}}
 owner:=map[string]string{"calendar.anton":"anton123"}
 input:=[]Event{
  {Title:"Badminton",SourceCalendar:"calendar.anton",External:true},
  {Title:"#Alma #Sport Biograf",SourceCalendar:"calendar.anton",External:true},
  {Title:"#ukendt Privat aftale",SourceCalendar:"calendar.anton",External:true},
 }
 result:=applyCalendarOwners(input,people,owner)
 if result[0].PersonID!="anton123"||len(result[0].PersonIDs)!=1 {t.Fatalf("default failed: %+v",result[0])}
 if result[1].PersonID!="alma123"||result[1].Title!="Biograf"||result[1].Category!="sport" {t.Fatalf("explicit tag should win: %+v",result[1])}
 if result[2].PersonID!="anton123"||!strings.Contains(result[2].Title,"#ukendt") {t.Fatalf("unknown tags lost: %+v",result[2])}
}

func TestCalendarOwnersEndpointsAndPersistence(t *testing.T){
 a,_:=testApp(t)
 a.cfg.HACalendars=[]string{"calendar.anton","calendar.alma"}
 s:=httptest.NewServer(securityHeaders(a.mux))
 defer s.Close()
 code,_,_:=req(t,s,"GET","/api/calendar-owners","","","")
 if code!=http.StatusUnauthorized {t.Fatalf("must be authenticated: %d",code)}
 cookie,csrf:=authenticated(t,s)
 code,_,_ = req(t,s,"PUT","/api/calendar-owners",`{"entity_id":"calendar.anton","person_id":""}`,cookie,"")
 if code!=http.StatusForbidden {t.Fatalf("CSRF is required: %d",code)}
 code,_,_ = req(t,s,"PUT","/api/calendar-owners",`{"entity_id":"calendar.other","person_id":""}`,cookie,csrf)
 if code!=http.StatusBadRequest {t.Fatalf("cannot write arbitrary entities: %d",code)}
 code,person,_:=req(t,s,"POST","/api/people",`{"name":"Anton","color":"#ddaa00","notify_service":"","position":0}`,cookie,csrf)
 if code!=200 {t.Fatalf("create person %d",code)}
 pid:=person["id"].(string)
 code,_,_ = req(t,s,"PUT","/api/calendar-owners",fmt.Sprintf(`{"entity_id":"calendar.anton","person_id":"%s"}`,pid),cookie,csrf)
 if code!=200 {t.Fatalf("save owner %d",code)}
 var response []CalendarOwner
 request,_:=http.NewRequest("GET",s.URL+"/api/calendar-owners",nil)
 request.Header.Set("Cookie",cookie)
 res,err:=http.DefaultClient.Do(request)
 if err!=nil {t.Fatal(err)}
 defer res.Body.Close()
 if err:=json.NewDecoder(res.Body).Decode(&response);err!=nil{t.Fatal(err)}
 if len(response)!=2||response[0].PersonID!=pid||response[1].PersonID!=""{t.Fatalf("owners: %+v",response)}
 if err:=a.db.Init();err!=nil{t.Fatal(err)}
 owners,err:=a.calendarOwnerMap()
 if err!=nil||owners["calendar.anton"]!=pid{t.Fatalf("mapping not persisted: %v %v",owners,err)}
}

func TestTodayPanelIndependentOfSelectedWeek(t *testing.T){
 a,_:=testApp(t)
 now:=time.Now().In(a.loc)
 begin:=time.Date(now.Year(),now.Month(),now.Day(),14,0,0,0,a.loc)
 end:=begin.Add(time.Hour)
 if err:=a.db.Exec("INSERT INTO events(id,title,start_at,end_at,kind) VALUES(?,?,?,?,?)",newID(),"Dagens begivenhed",begin.UTC().Format(time.RFC3339),end.UTC().Format(time.RFC3339),"aftale");err!=nil{t.Fatal(err)}
 s:=httptest.NewServer(securityHeaders(a.mux))
 defer s.Close()
 cookie,_:=authenticated(t,s)
 request,_:=http.NewRequest("GET",s.URL+"/api/state?start=2020-01-01T00:00:00Z&end=2020-01-08T00:00:00Z&day=2020-01-01",nil)
 request.Header.Set("Cookie",cookie)
 response,err:=http.DefaultClient.Do(request)
 if err!=nil{t.Fatal(err)}
 defer response.Body.Close()
 if response.StatusCode!=200{t.Fatalf("state failed: %d",response.StatusCode)}
 var data struct {
  TodayDate string `json:"today_date"`
  TodayEvents []Event `json:"today_events"`
  Events []Event `json:"events"`
 }
 if err:=json.NewDecoder(response.Body).Decode(&data);err!=nil{t.Fatal(err)}
 if data.TodayDate!=now.Format("2006-01-02"){t.Fatalf("today date wrong: %q",data.TodayDate)}
 if len(data.TodayEvents)!=1||data.TodayEvents[0].Title!="Dagens begivenhed"{t.Fatalf("today panel wrong: %+v",data.TodayEvents)}
 if len(data.Events)!=0{t.Fatalf("calendar should still show selected historical week: %+v",data.Events)}
}
