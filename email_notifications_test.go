package main

import (
 "strings"
 "testing"
 "net/http/httptest"
)

func TestSMTPSettingsAreAdminOnlyAndSecretsStayEncrypted(t *testing.T) {
 a,_:=testApp(t)
 server:=httptest.NewServer(securityHeaders(a.mux));defer server.Close()
 cookie,csrf:=authenticated(t,server)
 status,_,_=req(t,server,"PUT","/api/email/config",`{"host":"smtp.example.com","port":587,"username":"sender","from":"notify@example.com","password":"private-smtp-password"}`,cookie,"")
 if status!=403{t.Fatalf("SMTP config should require CSRF: %d",status)}
 status,_,_=req(t,server,"PUT","/api/email/config",`{"host":"smtp.example.com","port":587,"username":"sender","from":"notify@example.com","password":"private-smtp-password"}`,cookie,csrf)
 if status!=200{t.Fatalf("SMTP config not saved: %d",status)}
 status,data,_:=req(t,server,"GET","/api/email/config","",cookie,"")
 if status!=200||data["secret_set"]!=true{t.Fatalf("SMTP config: %d %+v",status,data)}
 if _,found:=data["password"];found{t.Fatal("SMTP response exposed cleartext password")}
 cfg,e:=a.emailSettings()
 if e!=nil||cfg.Ciphertext==""||strings.Contains(cfg.Ciphertext,"private-smtp-password"){t.Fatalf("SMTP password not encrypted: %+v %v",cfg,e)}
 status,person,_:=req(t,server,"POST","/api/people",`{"name":"Anton","color":"#ffcc00","notify_service":"","position":0}`,cookie,csrf)
 if status!=200{t.Fatalf("create person %d",status)}
 id:=person["id"].(string)
 status,_,_=req(t,server,"PUT","/api/email/recipient",`{"person_id":"missing","email":"someone@example.com"}`,cookie,csrf)
 if status!=400{t.Fatalf("bad recipient accepted: %d",status)}
 status,_,_=req(t,server,"PUT","/api/email/recipient",`{"person_id":"`+id+`","email":"anton@example.com"}`,cookie,csrf)
 if status!=200{t.Fatalf("recipient not saved: %d",status)}
 cfg,_=a.emailSettings()
 if len(cfg.Recipients)!=1||cfg.Recipients[0].Email!="anton@example.com"{t.Fatalf("recipient not stored: %+v",cfg.Recipients)}
 status,_,_=req(t,server,"PUT","/api/email/config",`{"host":"smtp.example.com","port":587,"username":"sender","from":"notify@example.com","password":"","clear_password":true}`,cookie,csrf)
 if status!=200{t.Fatalf("SMTP key removal failed: %d",status)}
 cfg,_=a.emailSettings()
 if cfg.Ciphertext!=""{t.Fatal("SMTP password removal did not work")}
}
func TestSMTPAndEmailAddressValidation(t *testing.T) {
 for _,mail:=range []string{"invalid","a@example.com\nCc: leaked@example.com","a@","Alice <alice@example.com>"}{
  if validEmail(mail){t.Fatalf("unsafe address accepted %q",mail)}
 }
 for _,mail:=range []string{"a@example.com","first.last+family@school.example.org"}{
  if !validEmail(mail){t.Fatalf("valid address rejected %q",mail)}
 }
 for _,host:=range []string{"smtp.example.com","localhost"}{
  if !validSMTPHost(host){t.Fatalf("valid host rejected %q",host)}
 }
 for _,host:=range []string{"smtp.example.com:587","user@server.example.com","a/b"}{
  if validSMTPHost(host){t.Fatalf("invalid host accepted %q",host)}
 }
}
