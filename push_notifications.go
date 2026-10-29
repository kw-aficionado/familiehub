package main

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "net/http"
 "net/url"
 "strings"
 "time"

 webpush "github.com/SherClockHolmes/webpush-go"
)

type pushKeys struct {
 Public string
 PrivateCipher string
}
type pushRequest struct{
 Endpoint string `json:"endpoint"`
 Keys struct{
  P256dh string `json:"p256dh"`
  Auth string `json:"auth"`
 } `json:"keys"`
 PersonID string `json:"person_id"`
}
func validPushEndpoint(raw string)bool {
 if len(raw)<20||len(raw)>2048{return false}
 u,err:=url.Parse(raw)
 if err!=nil||u.Scheme!="https"||u.Host==""||u.User!=nil||u.Fragment!=""{return false}
 host:=strings.ToLower(u.Hostname())
 for _,allowed:=range []string{"web.push.apple.com","fcm.googleapis.com","updates.push.services.mozilla.com","push.services.mozilla.com","updates.push.services.mozilla.org","notify.windows.com"}{
  if host==allowed {return true}
 }
 return strings.HasSuffix(host,".notify.windows.com")
}
func (a *App) pushKeyPair()(pushKeys,error){
 rows,err:=a.db.Query("SELECT public_key,private_cipher FROM push_settings WHERE id=1")
 if err!=nil{return pushKeys{},err}
 if len(rows)>0{return pushKeys{Public:rows[0]["public_key"],PrivateCipher:rows[0]["private_cipher"]},nil}
 privateKey,publicKey,err:=webpush.GenerateVAPIDKeys()
 if err!=nil{return pushKeys{},err}
 encrypted,err:=a.encryptAIKey(privateKey)
 if err!=nil{return pushKeys{},err}
 if err:=a.db.Exec("INSERT OR IGNORE INTO push_settings(id,public_key,private_cipher) VALUES(1,?,?)",publicKey,encrypted);err!=nil{return pushKeys{},err}
 rows,err=a.db.Query("SELECT public_key,private_cipher FROM push_settings WHERE id=1")
 if err!=nil||len(rows)!=1{return pushKeys{},errors.New("VAPID-nøgle kunne ikke gemmes")}
 return pushKeys{Public:rows[0]["public_key"],PrivateCipher:rows[0]["private_cipher"]},nil
}
func (a *App) pushPublicKey(w http.ResponseWriter,r *http.Request){
 keys,err:=a.pushKeyPair()
 if err!=nil{apiError(w,500,"Push er ikke tilgængeligt");return}
 user,ok:=a.accountIdentity(r)
 if !ok{apiError(w,401,"Log ind igen");return}
 writeJSON(w,200,map[string]any{"public_key":keys.Public,"person_id":user.PersonID,"is_admin":user.Admin})
}
func (a *App) pushSubscribe(w http.ResponseWriter,r *http.Request){
 user,ok:=a.accountIdentity(r)
 if !ok{apiError(w,401,"Log ind igen");return}
 var data pushRequest
 if !readJSON(w,r,&data){return}
 if !validPushEndpoint(data.Endpoint)||len(data.Keys.P256dh)>200||len(data.Keys.Auth)>100||
 data.Keys.P256dh==""||data.Keys.Auth=="" {
  apiError(w,400,"Ugyldigt push-abonnement");return
 }
 personID:=user.PersonID
 if user.Admin{personID=data.PersonID}
 if personID==""{apiError(w,400,"Vælg en person for dette abonnement");return}
 people,err:=a.people()
 if err!=nil{apiError(w,500,"Familiemedlemmer kan ikke læses");return}
 found:=false
 for _,p:=range people{if p.ID==personID{found=true;break}}
 if !found{apiError(w,400,"Ukendt familiemedlem");return}
 cipher,err:=a.encryptAIKey(data.Endpoint)
 if err!=nil{apiError(w,500,"Kunne ikke gemme abonnementet");return}
 hash:=sha(data.Endpoint)
 if err:=a.db.Exec("INSERT INTO push_subscriptions(id,account_id,person_id,endpoint_hash,endpoint_cipher,p256dh,auth,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(endpoint_hash) DO UPDATE SET account_id=excluded.account_id,person_id=excluded.person_id,endpoint_cipher=excluded.endpoint_cipher,p256dh=excluded.p256dh,auth=excluded.auth",
 newID(),user.AccountID,personID,hash,cipher,data.Keys.P256dh,data.Keys.Auth,time.Now().UTC().Format(time.RFC3339));err!=nil{
  apiError(w,500,"Kunne ikke gemme push-abonnementet");return
 }
 writeJSON(w,200,map[string]bool{"ok":true})
}
func (a *App) pushUnsubscribe(w http.ResponseWriter,r *http.Request){
 user,ok:=a.accountIdentity(r)
 if !ok{apiError(w,401,"Log ind igen");return}
 var data struct{Endpoint string `json:"endpoint"`}
 if !readJSON(w,r,&data){return}
 if !validPushEndpoint(data.Endpoint){apiError(w,400,"Ugyldigt abonnement");return}
 if user.Admin {
  _=a.db.Exec("DELETE FROM push_subscriptions WHERE endpoint_hash=?",sha(data.Endpoint))
 }else{
  _=a.db.Exec("DELETE FROM push_subscriptions WHERE endpoint_hash=? AND account_id=?",sha(data.Endpoint),user.AccountID)
 }
 writeJSON(w,200,map[string]bool{"ok":true})
}
func (a *App) sendPushForPerson(ctx context.Context,p Person,title,message string)error{
 rows,err:=a.db.Query("SELECT endpoint_hash,endpoint_cipher,p256dh,auth FROM push_subscriptions WHERE person_id=?",p.ID)
 if err!=nil{return err}
 if len(rows)==0{return errors.New("Ingen push-abonnementer")}
 keys,err:=a.pushKeyPair()
 if err!=nil{return err}
 privateKey,err:=a.decryptAIKey(keys.PrivateCipher)
 if err!=nil{return errors.New("Push-nøgle kunne ikke læses")}
 subscriber:=strings.TrimRight(a.cfg.AppURL,"/")
 if !strings.HasPrefix(subscriber,"https://"){return errors.New("Push kræver HTTPS APP_URL")}
 if len(message)>2200{message=string([]rune(message)[:minRune(len([]rune(message)),800)])}
 payload,_:=json.Marshal(map[string]string{"title":title,"body":message,"url":"/#opgaver"})
 succeeded:=false
 for _,row:=range rows{
  endpoint,e:=a.decryptAIKey(row["endpoint_cipher"])
  if e!=nil||!validPushEndpoint(endpoint){continue}
  subscription:=&webpush.Subscription{Endpoint:endpoint,Keys:webpush.Keys{P256dh:row["p256dh"],Auth:row["auth"]}}
  options:=&webpush.Options{Subscriber:subscriber,VAPIDPublicKey:keys.Public,VAPIDPrivateKey:privateKey,TTL:3600,HTTPClient:a.client}
  response,e:=webpush.SendNotificationWithContext(ctx,payload,subscription,options)
  if e!=nil{continue}
  response.Body.Close()
  if response.StatusCode==http.StatusGone||response.StatusCode==http.StatusNotFound {
   _=a.db.Exec("DELETE FROM push_subscriptions WHERE endpoint_hash=?",row["endpoint_hash"])
   continue
  }
  if response.StatusCode>=200&&response.StatusCode<300{succeeded=true}
 }
 if succeeded{return nil}
 return fmt.Errorf("Ingen pushbeskeder blev afleveret")
}
