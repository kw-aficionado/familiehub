package main

import (
 "net/http"
 "net/http/httptest"
 "testing"
 "encoding/json"
)

func TestPlannerSeedPreviewApplyAndRotation(t *testing.T){
 app,_:=testApp(t)
 server:=httptest.NewServer(securityHeaders(app.mux));defer server.Close()
 cookie,csrf:=authenticated(t,server)
 code,_,_:=req(t,server,"POST","/api/planner/seed",`{}`,cookie,"")
 if code!=http.StatusForbidden{t.Fatalf("seed must require CSRF: %d",code)}
 for i:=0;i<2;i++{
  code,out,_:=req(t,server,"POST","/api/planner/seed",`{}`,cookie,csrf)
  if code!=200{t.Fatalf("seed: %d %v",code,out)}
  if i==0&&out["created"].(float64)!=15{t.Fatalf("expected 15 templates: %v",out)}
  if i==1&&out["created"].(float64)!=0{t.Fatalf("seed must be idempotent: %v",out)}
 }
 people:=[]string{}
 for _,name:=range []string{"Teenager 1","Teenager 2"}{
  code,out,_:=req(t,server,"POST","/api/people",`{"name":"`+name+`","color":"#3999aa","notify_service":"","position":0}`,cookie,csrf)
  if code!=200{t.Fatalf("person: %v",out)}
  people=append(people,out["id"].(string))
 }
 archive,err:=app.db.Query("SELECT id FROM task_templates WHERE title=?", "Tømme og fylde opvaskemaskine")
 if err!=nil||len(archive)!=1{t.Fatalf("archive: %v %v",archive,err)}
 request:=PlannerRequest{People:people,Choices:[]PlannerChoice{{TemplateID:archive[0]["id"],TimesPerWeek:7}},StartDate:"2026-11-02",RotateWeeks:1}
 raw,_:=json.Marshal(request)
 code,preview,_:=req(t,server,"POST","/api/planner/preview",string(raw),cookie,csrf)
 if code!=200{t.Fatalf("preview: %v",preview)}
 assignments:=preview["assignments"].([]any)
 if len(assignments)!=7{t.Fatalf("expected seven days: %d",len(assignments))}
 code,applied,_:=req(t,server,"POST","/api/planner/apply",string(raw),cookie,csrf)
 if code!=200{t.Fatalf("apply: %v",applied)}
 day1,err:=app.tasks("2026-11-02");if err!=nil||len(day1)!=1{t.Fatalf("week1: %v %v",day1,err)}
 day2,err:=app.tasks("2026-11-09");if err!=nil||len(day2)!=1{t.Fatalf("week2: %v %v",day2,err)}
 if day1[0].AssignedTo==day2[0].AssignedTo{t.Fatalf("owners did not rotate: %v %v",day1,day2)}
 day3,err:=app.tasks("2026-11-16");if err!=nil||len(day3)!=1||day3[0].AssignedTo!=day1[0].AssignedTo{t.Fatalf("third week: %v %v",day3,err)}
}
func TestRotatedOwnerWithoutRotation(t *testing.T){
 if got:=rotatedOwner("person","","",0,"2026-11-09");got!="person"{t.Fatal(got)}
 if got:=rotatedOwner("a",`["a","b"]`,"2026-11-02",2,"2026-11-09");got!="a"{t.Fatal(got)}
 if got:=rotatedOwner("a",`["a","b"]`,"2026-11-02",2,"2026-11-16");got!="b"{t.Fatal(got)}
}

func TestPlannerApplyWorksForFamilyMemberAndShowsNextWeek(t *testing.T){
 app,_:=testApp(t)
 server:=httptest.NewServer(securityHeaders(app.mux));defer server.Close()
 adminCookie,adminCSRF:=authenticated(t,server)
 code,_,_:=req(t,server,"POST","/api/planner/seed",`{}`,adminCookie,adminCSRF)
 if code!=http.StatusOK{t.Fatalf("seed returned %d",code)}
 var people []string
 for _,name:=range []string{"Barn A","Barn B"}{
  code,p,_:=req(t,server,"POST","/api/people",`{"name":"`+name+`","color":"#3999aa","notify_service":"","position":0}`,adminCookie,adminCSRF)
  if code!=http.StatusOK{t.Fatalf("creating %s: %d %v",name,code,p)}
  people=append(people,p["id"].(string))
 }
 code,_,_ = req(t,server,"POST","/api/accounts",`{"username":"teenager","password":"long-sample-password","person_id":"","enabled":true}`,adminCookie,adminCSRF)
 if code!=http.StatusOK{t.Fatalf("creating member account: %d",code)}
 code,session,response:=req(t,server,"POST","/api/login",`{"username":"teenager","password":"long-sample-password"}`,"","")
 if code!=http.StatusOK{t.Fatalf("login: %d %v",code,session)}
 memberCookie:=response.Cookies()[0].Name+"="+response.Cookies()[0].Value
 memberCSRF:=session["csrf"].(string)
 rows,err:=app.db.Query("SELECT id FROM task_templates WHERE title=?","Tømme og fylde opvaskemaskine")
 if err!=nil||len(rows)!=1{t.Fatalf("template not found: %v %v",rows,err)}
 request:=PlannerRequest{People:people,Choices:[]PlannerChoice{{TemplateID:rows[0]["id"],TimesPerWeek:7}},StartDate:"2026-11-02",RotateWeeks:1}
 payload,err:=json.Marshal(request);if err!=nil{t.Fatal(err)}
 code,preview,_:=req(t,server,"POST","/api/planner/preview",string(payload),memberCookie,memberCSRF)
 if code!=http.StatusOK{t.Fatalf("member cannot preview: %d %v",code,preview)}
 // Preview is read-only.
 count,err:=app.db.Query("SELECT id FROM tasks")
 if err!=nil||len(count)!=0{t.Fatalf("preview wrote tasks: %v %v",count,err)}
 code,_,_=req(t,server,"POST","/api/planner/apply",string(payload),memberCookie,"")
 if code!=http.StatusForbidden{t.Fatalf("member save without CSRF must be forbidden: %d",code)}
 code,saved,_:=req(t,server,"POST","/api/planner/apply",string(payload),memberCookie,memberCSRF)
 if code!=http.StatusOK||saved["ok"]!=true||saved["created"]!=float64(7){t.Fatalf("member save: %d %v",code,saved)}
 code,week,_:=req(t,server,"GET","/api/tasks/week?start=2026-11-02","",memberCookie,"")
 if code!=http.StatusOK{t.Fatalf("fetch planned week: %d %v",code,week)}
 entries,ok:=week["entries"].([]any)
 if !ok||len(entries)!=7{t.Fatalf("planned week should contain seven tasks: %v",week)}
 code,_,_=req(t,server,"POST","/api/planner/seed",`{}`,memberCookie,memberCSRF)
 if code!=http.StatusForbidden{t.Fatalf("archive import should remain admin-only: %d",code)}
}
