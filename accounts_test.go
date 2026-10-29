package main

import (
 "encoding/hex"
 "fmt"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestAccountPasswordKDF(t *testing.T){
 got:=hex.EncodeToString(passwordKDF("password",[]byte("salt"),1))
 want:="120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"
 if got!=want{t.Fatalf("PBKDF2-SHA256 vector mismatch: got %s",got)}
 hash,err:=hashAccountPassword("a-very-long-secret-password")
 if err!=nil{t.Fatal(err)}
 if !checkAccountPassword("a-very-long-secret-password",hash)||checkAccountPassword("other-password",hash){
  t.Fatal("account password validation wrong")
 }
 if validAccountPassword("short")||!validAccountPassword("this-is-a-secure-password"){t.Fatal("password policy")}
}

func TestFamilyAccountsAdminLoginAndRevocation(t *testing.T){
 a,_:=testApp(t)
 s:=httptest.NewServer(securityHeaders(a.mux))
 defer s.Close()
 adminCookie,csrf:=authenticated(t,s)
 code,person,_:=req(t,s,"POST","/api/people",`{"name":"Anton","color":"#dca800","notify_service":"","position":0}`,adminCookie,csrf)
 if code!=200{t.Fatalf("person: %d %v",code,person)}
 pid:=person["id"].(string)

 // Creating user requires admin and CSRF.
 code,_,_ = req(t,s,"POST","/api/accounts",`{"username":"anton","password":"long-sample-password","person_id":"","enabled":true}`,adminCookie,"")
 if code!=403{t.Fatalf("csrf required for account creation: %d",code)}
 code,account,_:=req(t,s,"POST","/api/accounts",fmt.Sprintf(`{"username":"Anton","password":"long-sample-password","person_id":"%s","enabled":true}`,pid),adminCookie,csrf)
 if code!=200{t.Fatalf("create account: %d %v",code,account)}
 id:=account["id"].(string)
 if account["username"]!="anton"{t.Fatalf("username should normalize: %v",account)}
 code,_,_ = req(t,s,"POST","/api/accounts",`{"username":"Anton","password":"another-long-password","person_id":"","enabled":true}`,adminCookie,csrf)
 if code!=409{t.Fatalf("duplicate username not rejected: %d",code)}
 code,_,_ = req(t,s,"POST","/api/login",`{"username":"anton","password":"wrong-long-password"}`,"","")
 if code!=401{t.Fatalf("incorrect password accepted: %d",code)}
 code,login,response:=req(t,s,"POST","/api/login",`{"username":"anton","password":"long-sample-password"}`,"","")
 if code!=200{t.Fatalf("member login: %d %v",code,login)}
 memberCookie:=response.Cookies()[0].Name+"="+response.Cookies()[0].Value
 memberCSRF:=login["csrf"].(string)
 if login["account"].(map[string]any)["role"]!="member"{t.Fatalf("not member: %v",login)}
 code,_,_ = req(t,s,"GET","/api/accounts","",memberCookie,"")
 if code!=403{t.Fatalf("member can list accounts: %d",code)}
 code,_,_ = req(t,s,"POST","/api/accounts",`{"username":"stine","password":"long-sample-password","enabled":true,"person_id":""}`,memberCookie,memberCSRF)
 if code!=403{t.Fatalf("member can create account: %d",code)}
 code,session,_:=req(t,s,"GET","/api/session","",memberCookie,"")
 if code!=200||session["authenticated"]!=true{t.Fatalf("session missing: %v",session)}
 // Member continues to have regular calendar access.
 code,_,_ = req(t,s,"GET","/api/state?day=2026-10-28&start=2026-10-28T00:00:00Z&end=2026-11-02T00:00:00Z","",memberCookie,"")
 if code!=200{t.Fatalf("member cannot use calendar: %d",code)}

 // Admin reset revokes sessions immediately.
 code,_,_ = req(t,s,"POST","/api/accounts/"+id+"/password",`{"password":"new-account-password"}`,adminCookie,csrf)
 if code!=200{t.Fatalf("reset password: %d",code)}
 code,session,_=req(t,s,"GET","/api/session","",memberCookie,"")
 if code!=200||session["authenticated"]!=false{t.Fatalf("reset did not revoke sessions: %v",session)}
 code,_,_=req(t,s,"POST","/api/login",`{"username":"anton","password":"long-sample-password"}`,"","")
 if code!=401{t.Fatalf("old password accepted: %d",code)}
 code,login,response=req(t,s,"POST","/api/login",`{"username":"anton","password":"new-account-password"}`,"","")
 if code!=200{t.Fatalf("new password login: %d %v",code,login)}
 memberCookie=response.Cookies()[0].Name+"="+response.Cookies()[0].Value
 memberCSRF=login["csrf"].(string)
 // Change own password closes current session as well.
 code,_,_=req(t,s,"POST","/api/account/password",`{"old_password":"bad","new_password":"third-account-password"}`,memberCookie,memberCSRF)
 if code!=401{t.Fatalf("wrong old password was accepted: %d",code)}
 code,_,_=req(t,s,"POST","/api/account/password",`{"old_password":"new-account-password","new_password":"third-account-password"}`,memberCookie,memberCSRF)
 if code!=200{t.Fatalf("member change password: %d",code)}
 code,session,_=req(t,s,"GET","/api/session","",memberCookie,"")
 if session["authenticated"]!=false{t.Fatalf("change password must revoke session: %v",session)}

 // Existing administrator can still log in without a username.
 code,_,_=req(t,s,"GET","/api/accounts","",adminCookie,"")
 if code!=200{t.Fatalf("legacy admin lost admin access: %d",code)}
}

func TestFamilyAccountDisableAndUpgrade(t *testing.T){
 a,_:=testApp(t)
 s:=httptest.NewServer(a.mux)
 defer s.Close()
 cookie,csrf:=authenticated(t,s)
 code,account,_:=req(t,s,"POST","/api/accounts",`{"username":"stine","password":"long-sample-password","person_id":"","enabled":true}`,cookie,csrf)
 if code!=200{t.Fatalf("create account: %d",code)}
 id:=account["id"].(string)
 code,_,_=req(t,s,"PUT","/api/accounts/"+id,`{"username":"stine","password":"","person_id":"","enabled":false}`,cookie,csrf)
 if code!=200{t.Fatalf("disable account: %d",code)}
 code,_,_=req(t,s,"POST","/api/login",`{"username":"stine","password":"long-sample-password"}`,"","")
 if code!=401{t.Fatalf("disabled login allowed: %d",code)}
 if err:=a.db.Init();err!=nil{t.Fatalf("additive migration is not idempotent: %v",err)}
 rows,err:=a.db.Query("SELECT username FROM accounts WHERE id=?",id)
 if err!=nil||len(rows)!=1||!strings.EqualFold(rows[0]["username"],"stine"){t.Fatalf("account lost during Init: %v %v",rows,err)}
}
