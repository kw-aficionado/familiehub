package main

import (
 "context"
 "encoding/json"
 "io"
 "net/http"
 "net/http/httptest"
 "net/url"
 "strings"
 "testing"
 "time"
)

func TestDirectCalendarValidationAndAuth(t *testing.T) {
 a,_:=testApp(t)
 s:=httptest.NewServer(securityHeaders(a.mux));defer s.Close()
 cookie,csrf:=authenticated(t,s)
 for _,input:=range []string{
  "http://example.com/cal.ics","https://127.0.0.1/cal.ics","https://localhost/cal.ics",
  "https://example.com/private.ics?token=abc","https://user:pass@example.com/cal.ics",
 }{
  if validCalendarEndpoint(input){t.Fatalf("unsafe URL accepted: %q",input)}
 }
 if !validCalendarEndpoint("https://calendar.example.org/family.ics"){t.Fatal("HTTPS ICS feed rejected")}
 status,_,_=req(t,s,"POST","/api/direct-calendars",`{"provider":"ics","name":"Privat kalender","url":"https://example.com/family.ics","person_id":"","secret":""}`,cookie,"")
 if status!=403{t.Fatalf("CSRF not enforced: %d",status)}
 status,created,_:=req(t,s,"POST","/api/direct-calendars",`{"provider":"ics","name":"Familien","url":"https://example.org/family.ics","person_id":"","secret":""}`,cookie,csrf)
 if status!=201{t.Fatalf("ICS connection not created: %d",status)}
 if created["provider"]!="ics"{t.Fatalf("wrong provider: %v",created)}
 status,_,_=req(t,s,"POST","/api/direct-calendars",`{"provider":"icloud","name":"Apple","url":"https://caldav.icloud.com/p/a/","username":"parent@example.org","secret":""}`,cookie,csrf)
 if status!=400{t.Fatalf("missing app password accepted: %d",status)}
 status,cal,_:=req(t,s,"POST","/api/direct-calendars",`{"provider":"icloud","name":"Apple","url":"https://caldav.icloud.com/p/a/","username":"parent@example.org","secret":"app-password-secret"}`,cookie,csrf)
 if status!=201{t.Fatalf("CalDAV connection failed: %d",status)}
 id:=cal["id"].(string)
 rows,err:=a.db.Query("SELECT secret_cipher FROM direct_calendars WHERE id=?",id)
 if err!=nil||len(rows)!=1||strings.Contains(rows[0]["secret_cipher"],"app-password"){t.Fatalf("secret not encrypted %v %v",err,rows)}
 status,_,_=req(t,s,"POST","/api/accounts",`{"username":"calendarreader","password":"long-reader-password","person_id":"","enabled":true}`,cookie,csrf)
 if status!=200{t.Fatalf("create member: %d",status)}
 status,memberLogin,login:=req(t,s,"POST","/api/login",`{"username":"calendarreader","password":"long-reader-password"}`,"","")
 if status!=200{t.Fatalf("member login: %d",status)}
 memberCookie:=login.Cookies()[0].Name+"="+login.Cookies()[0].Value
 status,_,_=req(t,s,"GET","/api/direct-calendars","",memberCookie,"")
 if status!=403{t.Fatalf("member must not inspect connections: %d",status)}
 status,_,_=req(t,s,"DELETE","/api/direct-calendars/"+id,"",memberCookie,loginToken(memberLogin))
 if status!=403{t.Fatalf("member cannot delete connection: %d",status)}
}
func loginToken(v map[string]any)string {
 return v["csrf"].(string)
}
func TestDirectCalendarOAuthStateAndCallback(t *testing.T) {
 a,_:=testApp(t)
 a.cfg.AppURL="https://familie.example.com"
 s:=httptest.NewServer(securityHeaders(a.mux));defer s.Close()
 cookie,csrf:=authenticated(t,s)
 status,created,_:=req(t,s,"POST","/api/direct-calendars",`{"provider":"google","name":"Google familie","client_id":"test-client","secret":"client-password","person_id":""}`,cookie,csrf)
 if status!=201{t.Fatalf("create OAuth config: %d",status)}
 id:=created["id"].(string)
 status,auth,_:=req(t,s,"POST","/api/direct-calendars/"+id+"/authorize",`{}`,cookie,csrf)
 if status!=200{t.Fatalf("authorize: %d",status)}
 u,err:=url.Parse(auth["authorization_url"].(string))
 if err!=nil||u.Host!="accounts.google.com"||u.Query().Get("scope")!="https://www.googleapis.com/auth/calendar.readonly"||u.Query().Get("redirect_uri")!=a.oauthRedirect(){t.Fatalf("OAuth URL: %v %v",u,err)}
 state:=u.Query().Get("state")
 if len(state)!=64{t.Fatal("state must contain cryptographic entropy")}
 a.client=&http.Client{Transport:roundTripTestFunc(func(req *http.Request)(*http.Response,error){
  if req.URL.Host!="oauth2.googleapis.com"{t.Fatalf("unexpected OAuth endpoint %v",req.URL)}
  data:=`{"access_token":"access-secret","refresh_token":"refresh-secret"}`
  return &http.Response{StatusCode:200,Body:io.NopCloser(strings.NewReader(data)),Header:make(http.Header)},nil
 })}
 callback:=func(state string)*http.Response{
  req,_:=http.NewRequest("GET",s.URL+"/api/direct-calendars/oauth/callback?state="+url.QueryEscape(state)+"&code=authorized-code",nil)
  req.Header.Set("Cookie",cookie)
  cli:=http.Client{CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}
  resp,err:=cli.Do(req)
  if err!=nil{t.Fatal(err)}
  return resp
 }
 resp:=callback("wrong")
 resp.Body.Close()
 if resp.StatusCode!=400&&resp.StatusCode!=403{t.Fatalf("invalid state accepted: %d",resp.StatusCode)}
 resp=callback(state);resp.Body.Close()
 if resp.StatusCode!=303{t.Fatalf("valid callback: %d",resp.StatusCode)}
 resp=callback(state);resp.Body.Close()
 if resp.StatusCode!=403{t.Fatalf("replayed state accepted: %d",resp.StatusCode)}
 saved,err:=a.directCalendarByID(id)
 if err!=nil||saved.Refresh==""||strings.Contains(saved.Refresh,"refresh-secret"){t.Fatalf("OAuth secrets were not saved securely: %+v %v",saved,err)}
}
func TestICSCalendarImportAndColour(t *testing.T) {
 loc,_:=time.LoadLocation("Europe/Copenhagen")
 row:=calendarRecord{CalendarConnection:CalendarConnection{ID:"source1",Provider:"ics",Name:"Person kalender",PersonID:"anton"}}
 raw:="BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:event1\r\nSUMMARY:Badminton\r\nDTSTART;TZID=Europe/Copenhagen:20261029T180000\r\nDTEND;TZID=Europe/Copenhagen:20261029T193000\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
 from:=time.Date(2026,10,29,0,0,0,0,loc);to:=from.AddDate(0,0,1)
 results:=parseICS(raw,row,from,to,loc)
 if len(results)!=1||results[0].Title!="Badminton"||results[0].PersonID!="anton"{t.Fatalf("ICS parser: %+v",results)}
 expected:=time.Date(2026,10,29,18,0,0,0,loc).Format(time.RFC3339)
 if results[0].Start!=expected{t.Fatalf("wrong Copenhagen DST offset: got %s want %s",results[0].Start,expected)}
 if len(parseICS(raw,row,from.AddDate(0,0,7),to.AddDate(0,0,7),loc))!=0{t.Fatal("ICS must filter outside period")}
}
func TestDirectGoogleAndMicrosoftReaders(t *testing.T) {
 a,_:=testApp(t)
 provider:=[]string{}
 a.client=&http.Client{Transport:roundTripTestFunc(func(r *http.Request)(*http.Response,error){
  provider=append(provider,r.URL.Host)
  body:="{}"
  switch r.URL.Host{
  case "oauth2.googleapis.com","login.microsoftonline.com":
   body=`{"access_token":"short-token"}`
  case "www.googleapis.com":
   body=`{"items":[{"id":"e1","summary":"Træning","start":{"dateTime":"2026-10-29T16:00:00+01:00"},"end":{"dateTime":"2026-10-29T17:00:00+01:00"}}]}`
  case "graph.microsoft.com":
   body=`{"value":[{"id":"e2","subject":"Møde","start":{"dateTime":"2026-10-29T12:00:00"},"end":{"dateTime":"2026-10-29T13:00:00"}}]}`
  }
  return &http.Response{StatusCode:200,Body:io.NopCloser(strings.NewReader(body)),Header:make(http.Header)},nil
 })}
 from:=time.Date(2026,10,29,0,0,0,0,time.UTC);to:=from.AddDate(0,0,1)
 for _,kind:=range []string{"google","microsoft"}{
  encrypted,_:=a.encryptAIKey("secret")
  refresh,_:=a.encryptAIKey("refresh")
  row:=calendarRecord{CalendarConnection:CalendarConnection{ID:"source1",Provider:kind,Name:kind,ClientID:"client"},Secret:encrypted,Refresh:refresh}
  events,err:=a.fetchDirectCalendar(context.Background(),row,from,to)
  if err!=nil||len(events)!=1{t.Fatalf("%s events: %d %v",kind,len(events),err)}
 }
 if len(provider)!=4{t.Fatalf("unexpected requests: %+v",provider)}
}
