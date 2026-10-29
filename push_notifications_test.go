package main

import (
 "encoding/base64"
 "crypto/elliptic"
 "crypto/rand"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestPushEndpointAllowlist(t *testing.T){
 for _,u:=range []string{"https://web.push.apple.com/Q6/abcd1234567","https://fcm.googleapis.com/fcm/send/test-abcd123456","https://updates.push.services.mozilla.com/wpush/v2/testtoken"}{
  if !validPushEndpoint(u){t.Fatalf("valid push provider rejected: %s",u)}
 }
 for _,u:=range []string{"http://127.0.0.1:1234/notify","https://localhost/admin","https://fcm.googleapis.com.evil.example/hello","https://169.254.169.254/secret","https://user:password@web.push.apple.com/secret"}{
  if validPushEndpoint(u){t.Fatalf("unsafe push endpoint accepted: %s",u)}
 }
}
func TestPushKeyAndSubscriptionSecurity(t *testing.T){
 a,_:=testApp(t)
 server:=httptest.NewServer(securityHeaders(a.mux));defer server.Close()
 adminCookie,csrf:=authenticated(t,server)
 code,person,_:=req(t,server,"POST","/api/people",`{"name":"Anton","color":"#ddeeff","notify_service":"","position":0}`,adminCookie,csrf)
 if code!=200{t.Fatalf("create person: %d",code)}
 id:=person["id"].(string)
 code,_,_=req(t,server,"GET","/api/push/key","",adminCookie,"")
 if code!=200{t.Fatalf("push key endpoint: %d",code)}
 keys,err:=a.pushKeyPair()
 if err!=nil||keys.Public==""||keys.PrivateCipher==""{t.Fatalf("push VAPID creation: %+v %v",keys,err)}
 raw,e:=a.decryptAIKey(keys.PrivateCipher)
 if e!=nil||raw==""{t.Fatalf("VAPID encryption: %v",e)}
 code,_,_=req(t,server,"POST","/api/accounts",`{"username":"antonpush","password":"long-secret-password","person_id":"`+id+`","enabled":true}`,adminCookie,csrf)
 if code!=200{t.Fatalf("create member: %d",code)}
 code,data,response:=req(t,server,"POST","/api/login",`{"username":"antonpush","password":"long-secret-password"}`,"","")
 if code!=200{t.Fatalf("login member: %d",code)}
 cookie:=response.Cookies()[0].Name+"="+response.Cookies()[0].Value
 csrf2:=data["csrf"].(string)
 _,x,y,err:=elliptic.GenerateKey(elliptic.P256(),rand.Reader)
 if err!=nil{t.Fatal(err)}
 public:=base64.RawURLEncoding.EncodeToString(elliptic.Marshal(elliptic.P256(),x,y))
 auth:=base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef"))
 endpoint:="https://fcm.googleapis.com/fcm/send/test-secret-endpoint"
 payload:=`{"endpoint":"`+endpoint+`","keys":{"p256dh":"`+public+`","auth":"`+auth+`"},"person_id":"fake-other-person"}`
 code,_,_=req(t,server,"POST","/api/push/subscribe",payload,cookie,"")
 if code!=403{t.Fatalf("subscription needs CSRF: %d",code)}
 code,_,_=req(t,server,"POST","/api/push/subscribe",payload,cookie,csrf2)
 if code!=200{t.Fatalf("subscription failed: %d",code)}
 subs,err:=a.db.Query("SELECT person_id,endpoint_cipher FROM push_subscriptions")
 if err!=nil||len(subs)!=1||subs[0]["person_id"]!=id||strings.Contains(subs[0]["endpoint_cipher"],"fcm.googleapis.com"){
  t.Fatalf("push subscription must belong to current user and be encrypted: %+v %v",subs,err)
 }
 code,_,_=req(t,server,"POST","/api/push/unsubscribe",`{"endpoint":"`+endpoint+`"}`,cookie,csrf2)
 if code!=200{t.Fatalf("unsubscribe: %d",code)}
 subs,_=a.db.Query("SELECT id FROM push_subscriptions")
 if len(subs)!=0{t.Fatalf("subscription not deleted: %+v",subs)}
}
