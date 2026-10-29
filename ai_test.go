package main

import (
 "context"
 "io"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
 "time"
)

func TestAIConfigSecretsAndAdminOnly(t *testing.T){
 a,_:=testApp(t)
 server:=httptest.NewServer(securityHeaders(a.mux));defer server.Close()
 cookie,csrf:=authenticated(t,server)
 status,config,_:=req(t,server,"GET","/api/ai/config","",cookie,"")
 if status!=200||config["provider"]!="off"||config["reminders_enabled"]!=false{
  t.Fatalf("AI must start disabled: %d %+v",status,config)
 }
 status,_,_=req(t,server,"PUT","/api/ai/config",`{"provider":"openai","model":"gpt-4.1-mini","api_key":"very-private-12345","base_url":"","reminders_enabled":true,"reminder_time":"18:00"}`,cookie,"")
 if status!=403 {t.Fatalf("AI config mutation needs CSRF: %d",status)}
 status,_,_=req(t,server,"PUT","/api/ai/config",`{"provider":"openai","model":"gpt-4.1-mini","api_key":"very-private-12345","base_url":"","reminders_enabled":true,"reminder_time":"18:00"}`,cookie,csrf)
 if status!=200 {t.Fatalf("config save failed: %d",status)}
 status,config,_=req(t,server,"GET","/api/ai/config","",cookie,"")
 if status!=200||config["key_set"]!=true||config["reminders_enabled"]!=true{t.Fatalf("saved status: %+v",config)}
 if _,ok:=config["api_key"];ok {t.Fatal("API key leaked in response")}
 rows,err:=a.db.Query("SELECT api_key_cipher FROM ai_settings WHERE id=1")
 if err!=nil||len(rows)!=1||strings.Contains(rows[0]["api_key_cipher"],"very-private"){t.Fatalf("API key not encrypted at rest: %v %+v",err,rows)}
 saved,err:=a.loadAIConfig()
 if err!=nil{t.Fatal(err)}
 plain,err:=a.decryptAIKey(saved.Ciphertext)
 if err!=nil||plain!="very-private-12345"{t.Fatalf("encrypted secret not readable: %v",err)}
 status,_,_=req(t,server,"PUT","/api/ai/config",`{"provider":"openai","model":"gpt-4.1-mini","api_key":"","base_url":"","reminders_enabled":true,"reminder_time":"19:00"}`,cookie,csrf)
 if status!=200{t.Fatalf("preserve secret save: %d",status)}
 again,_:=a.loadAIConfig()
 if again.Ciphertext!=saved.Ciphertext{t.Fatal("blank API key must preserve encrypted secret")}
 status,_,_=req(t,server,"PUT","/api/ai/config",`{"provider":"compatible","model":"test","base_url":"http://example.com/v1","reminders_enabled":false,"reminder_time":"19:00"}`,cookie,csrf)
 if status!=400{t.Fatalf("public HTTP API must be rejected: %d",status)}
 status,_,_=req(t,server,"PUT","/api/ai/config",`{"provider":"openai","model":"gpt-4.1-mini","api_key":"","clear_key":true,"reminders_enabled":false,"reminder_time":"19:00"}`,cookie,csrf)
 if status!=200{t.Fatalf("clear key: %d",status)}
 cleared,_:=a.loadAIConfig()
 if cleared.KeySet||cleared.Ciphertext!=""{t.Fatal("clear key failed")}
 // A member can request AI assistance but cannot read or change provider secrets.
 status,person,_:=req(t,server,"POST","/api/accounts",`{"username":"reader","password":"long-reader-password","person_id":"","enabled":true}`,cookie,csrf)
 if status!=200{t.Fatalf("create member: %d %v",status,person)}
 status,_,login:=req(t,server,"POST","/api/login",`{"username":"reader","password":"long-reader-password"}`,"","")
 if status!=200{t.Fatalf("login member: %d",status)}
 memberCookie:=login.Cookies()[0].Name+"="+login.Cookies()[0].Value
 status,_,_=req(t,server,"GET","/api/ai/config","",memberCookie,"")
 if status!=403{t.Fatalf("member could read secret configuration: %d",status)}
}
func TestAIProviderProtocolAndFriendlyResponse(t *testing.T){
 a,_:=testApp(t)
 var gotPath,gotAuth string
 provider:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  gotPath=r.URL.Path;gotAuth=r.Header.Get("Authorization")
  w.Header().Set("Content-Type","application/json")
  io.WriteString(w,`{"choices":[{"message":{"content":"Husk venligt opgaverne."}}]}`)
 }))
 defer provider.Close()
 cfg:=aiStored{AIConfig:AIConfig{Provider:"compatible",Model:"test-model",BaseURL:provider.URL+"/v1"}}
 key,err:=a.encryptAIKey("test-key")
 if err!=nil{t.Fatal(err)}
 cfg.Ciphertext=key;cfg.KeySet=true
 msg,err:=a.aiGenerate(context.Background(),cfg,"A system message","Hello")
 if err!=nil||msg!="Husk venligt opgaverne."{t.Fatalf("compatible AI failed: %q %v",msg,err)}
 if gotPath!="/v1/chat/completions"||gotAuth!="Bearer test-key"{t.Fatalf("wrong endpoint/auth %q %q",gotPath,gotAuth)}
 // Exercise the Anthropic wire format without making a real internet request.
 a.client=&http.Client{Transport:roundTripTestFunc(func(r *http.Request)(*http.Response,error){
  if r.URL.Host!="api.anthropic.com"||r.URL.Path!="/v1/messages"||r.Header.Get("x-api-key")!="test-key"||r.Header.Get("anthropic-version")==""{
   t.Errorf("bad Claude request: %v",r)
  }
  return &http.Response{StatusCode:200,Body:io.NopCloser(strings.NewReader(`{"content":[{"type":"text","text":"Godt gået!"}]}`)),Header:http.Header{"Content-Type":[]string{"application/json"}}},nil
 })}
 cfg.Provider="anthropic";cfg.BaseURL="";cfg.Model="claude-test"
 msg,err=a.aiGenerate(context.Background(),cfg,"Be kind","Hi")
 if err!=nil||msg!="Godt gået!"{t.Fatalf("Anthropic response: %q %v",msg,err)}
}
type roundTripTestFunc func(*http.Request)(*http.Response,error)
func (fn roundTripTestFunc)RoundTrip(r *http.Request)(*http.Response,error){return fn(r)}

func TestAIPendingTasksAndDeduplicatedPush(t *testing.T){
 a,_:=testApp(t)
 now:=time.Date(2026,time.October,29,18,0,0,0,time.UTC)
 if err:=a.db.Exec("INSERT INTO people(id,name,color,notify_service,position) VALUES(?,?,?,?,?)","p1","Familie","#cc8822","mobile_app_family","0");err!=nil{t.Fatal(err)}
 for _,task:=range []struct{id,name,due,rule string}{
  {"t1","Tømme opvaskemaskine","2026-10-27","weekdays"},
  {"t2","Vande planter","2026-10-28","none"},
 }{
  if err:=a.db.Exec("INSERT INTO tasks(id,title,assigned_to,due_date,repeat_rule) VALUES(?,?,?,?,?)",task.id,task.name,"p1",task.due,task.rule);err!=nil{t.Fatal(err)}
 }
 if err:=a.db.Exec("INSERT INTO completions(task_id,day) VALUES(?,?)","t1","2026-10-28");err!=nil{t.Fatal(err)}
 items,err:=a.pendingTasks(now,7,true)
 if err!=nil{t.Fatal(err)}
 if len(items)!=2{t.Fatalf("expected two pending task titles, got %+v",items)}
 for _,item:=range items{if item.TaskID=="t1"&&item.Missed!=2{t.Fatalf("weekday counts incorrect: %+v",item)}}
 pushes:=0
 external:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if strings.HasPrefix(r.URL.Path,"/api/services/notify/"){pushes++;w.WriteHeader(200);io.WriteString(w,"{}");return}
  w.WriteHeader(404)
 }))
 defer external.Close()
 a.cfg.HAURL=external.URL;a.cfg.HAToken="test-ha-token"
 if err:=a.db.Exec("INSERT INTO ai_settings(id,provider,model,base_url,api_key_cipher,reminders_enabled,reminder_time) VALUES(1,'off','','','','1','18:00')");err!=nil{t.Fatal(err)}
 people:=map[string]Person{"p1":{ID:"p1",Name:"Familie",NotifyService:"mobile_app_family"}}
 a.aiReminders(now,people)
 a.aiReminders(now,people)
 if pushes!=1{t.Fatalf("daily push must dedupe across restarts: got %d",pushes)}
 a.aiReminders(now.Add(10*time.Minute),people)
 if pushes!=1{t.Fatalf("unplanned reminder minute: got %d",pushes)}
}
