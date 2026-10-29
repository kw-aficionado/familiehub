package main

import (
 "context"
 "crypto/rand"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "net/url"
 "strings"
 "time"
)

type CalendarConnection struct {
 ID string `json:"id"`
 Provider string `json:"provider"`
 Name string `json:"name"`
 PersonID string `json:"person_id"`
 URL string `json:"url,omitempty"`
 Username string `json:"username,omitempty"`
 ClientID string `json:"client_id,omitempty"`
 Connected bool `json:"connected"`
 HasSecret bool `json:"has_secret"`
 Status string `json:"status"`
}
type calendarRecord struct {
 CalendarConnection
 Secret string
 Refresh string
 Access string
}
func validCalendarEndpoint(raw string)bool{
 u,e:=url.Parse(raw)
 if e!=nil||u.Scheme!="https"||u.Host==""||u.User!=nil||u.Fragment!=""||u.RawQuery!="" {return false}
 h:=strings.ToLower(u.Hostname())
 if h=="localhost"||strings.HasSuffix(h,".localhost")||strings.HasSuffix(h,".internal")||strings.HasSuffix(h,".local"){return false}
 if ip:=net.ParseIP(h);ip!=nil&&(ip.IsLoopback()||ip.IsPrivate()||ip.IsUnspecified()||ip.IsLinkLocalUnicast()){return false}
 return len(raw)<=1024
}
func (a *App) calendarRecords()([]calendarRecord,error){
 rows,err:=a.db.Query("SELECT id,provider,name,person_id,endpoint,username,client_id,secret_cipher,access_cipher,refresh_cipher FROM direct_calendars ORDER BY name")
 if err!=nil{return nil,err}
 out:=[]calendarRecord{}
 for _,r:=range rows {
  row:=calendarRecord{CalendarConnection:CalendarConnection{ID:r["id"],Provider:r["provider"],Name:r["name"],PersonID:r["person_id"],URL:r["endpoint"],Username:r["username"],ClientID:r["client_id"]},Secret:r["secret_cipher"],Access:r["access_cipher"],Refresh:r["refresh_cipher"]}
  row.HasSecret=row.Secret!=""
  row.Connected=row.Provider=="ics"||row.Provider=="icloud"&&row.Secret!=""||row.Refresh!=""
  if row.Connected{row.Status="Forbundet"}else{row.Status="Afventer forbindelse"}
  out=append(out,row)
 }
 return out,nil
}
func (a *App) directCalendarsGet(w http.ResponseWriter,r *http.Request){
 rows,err:=a.calendarRecords();if err!=nil{apiError(w,500,"Kalenderforbindelser kunne ikke læses");return}
 result:=[]CalendarConnection{}
 for _,row:=range rows{result=append(result,row.CalendarConnection)}
 writeJSON(w,200,result)
}
func (a *App) directCalendarsSave(w http.ResponseWriter,r *http.Request){
 var v struct{
  Provider string `json:"provider"`
  Name string `json:"name"`
  PersonID string `json:"person_id"`
  URL string `json:"url"`
  Username string `json:"username"`
  ClientID string `json:"client_id"`
  Secret string `json:"secret"`
 }
 if !readJSON(w,r,&v){return}
 v.Provider=strings.ToLower(strings.TrimSpace(v.Provider));v.Name=strings.TrimSpace(v.Name);v.URL=strings.TrimSpace(v.URL)
 v.Username=strings.TrimSpace(v.Username);v.ClientID=strings.TrimSpace(v.ClientID)
 if len(v.Name)<2||len(v.Name)>80{apiError(w,400,"Kalendernavnet skal have 2–80 tegn");return}
 if v.Provider!="google"&&v.Provider!="microsoft"&&v.Provider!="ics"&&v.Provider!="icloud"{apiError(w,400,"Ukendt kalenderudbyder");return}
 if v.PersonID!=""{
  people,err:=a.people();if err!=nil{apiError(w,500,"Familiemedlemmer kunne ikke hentes");return}
  found:=false;for _,p:=range people{if p.ID==v.PersonID{found=true;break}}
  if !found{apiError(w,400,"Ukendt familiemedlem");return}
 }
 if len(v.Username)>150||len(v.ClientID)>300||len(v.Secret)>4096{apiError(w,400,"Et felt er for langt");return}
 if v.Provider=="ics"||v.Provider=="icloud"{
  if !validCalendarEndpoint(v.URL){apiError(w,400,"Angiv kalenderens HTTPS-adresse; interne adresser er ikke tilladt");return}
  if v.Provider=="icloud"{
   endpoint,_:=url.Parse(v.URL)
   if !strings.HasSuffix(strings.ToLower(endpoint.Hostname()),".icloud.com"){apiError(w,400,"iCloud CalDAV-adressen skal tilhøre icloud.com");return}
   if v.Username==""||v.Secret==""{apiError(w,400,"iCloud kræver Apple ID og en app-specifik adgangskode");return}
  }
 }else {
  if v.ClientID==""||v.Secret==""{apiError(w,400,"OAuth kræver client ID og client secret fra udbyderen");return}
  if v.URL!=""{apiError(w,400,"URL er kun til ICS og iCloud");return}
 }
 secret:=""
 if v.Secret!=""{
  var e error
  secret,e=a.encryptAIKey(v.Secret)
  if e!=nil{apiError(w,500,"Kunne ikke kryptere legitimationsoplysninger");return}
 }
 id:=newID()
 if err:=a.db.Exec("INSERT INTO direct_calendars(id,provider,name,person_id,endpoint,username,client_id,secret_cipher,access_cipher,refresh_cipher) VALUES(?,?,?,?,?,?,?,?,?,?)",id,v.Provider,v.Name,v.PersonID,v.URL,v.Username,v.ClientID,secret,"","");err!=nil{
  apiError(w,500,"Kunne ikke oprette kalenderforbindelsen");return
 }
 a.broadcast()
 writeJSON(w,201,CalendarConnection{ID:id,Provider:v.Provider,Name:v.Name,PersonID:v.PersonID,URL:v.URL,Username:v.Username,ClientID:v.ClientID,HasSecret:secret!="",Connected:v.Provider=="ics"||v.Provider=="icloud",Status:"Oprettet"})
}
func (a *App) directCalendarsDelete(w http.ResponseWriter,r *http.Request){
 id:=r.PathValue("id")
 if !validID(id){apiError(w,400,"Ugyldigt kalender-ID");return}
 if err:=a.db.Exec("DELETE FROM direct_calendars WHERE id=?",id);err!=nil{apiError(w,500,"Kalenderen kunne ikke fjernes");return}
 _=a.db.Exec("DELETE FROM direct_oauth_states WHERE calendar_id=?",id)
 a.broadcast();writeJSON(w,200,map[string]bool{"ok":true})
}
func (a *App) directCalendarByID(id string)(calendarRecord,error){
 rows,err:=a.calendarRecords();if err!=nil{return calendarRecord{},err}
 for _,row:=range rows{if row.ID==id{return row,nil}}
 return calendarRecord{},errors.New("forbindelsen findes ikke")
}
func oauthEndpoints(provider string)(auth,token,scope string){
 switch provider{
 case "google":
  return "https://accounts.google.com/o/oauth2/v2/auth","https://oauth2.googleapis.com/token","https://www.googleapis.com/auth/calendar.readonly"
 case "microsoft":
  return "https://login.microsoftonline.com/common/oauth2/v2.0/authorize","https://login.microsoftonline.com/common/oauth2/v2.0/token","offline_access Calendars.Read User.Read"
 }
 return "","",""
}
func (a *App) oauthRedirect()string {
 base:=strings.TrimRight(a.cfg.AppURL,"/")
 if !strings.HasPrefix(base,"https://"){return ""}
 return base+"/api/direct-calendars/oauth/callback"
}
func (a *App) directCalendarAuthorize(w http.ResponseWriter,r *http.Request){
 id:=r.PathValue("id")
 row,err:=a.directCalendarByID(id)
 if err!=nil{apiError(w,404,"Forbindelsen findes ikke");return}
 auth,_,scope:=oauthEndpoints(row.Provider)
 callback:=a.oauthRedirect()
 if auth==""||callback==""{apiError(w,400,"OAuth kræver APP_URL med HTTPS og en Google- eller Microsoft-kalender");return}
 nonce:=make([]byte,32);if _,err=rand.Read(nonce);err!=nil{apiError(w,500,"Kunne ikke starte OAuth");return}
 state:=hex.EncodeToString(nonce)
 session:=sha(a.sessionID(r))
 if err=a.db.Exec("INSERT INTO direct_oauth_states(state_hash,calendar_id,session_hash,expires_at) VALUES(?,?,?,?)",sha(state),id,session,time.Now().Add(10*time.Minute).UTC().Format(time.RFC3339));err!=nil{
  apiError(w,500,"Kunne ikke gemme OAuth-status");return
 }
 u,_:=url.Parse(auth)
 q:=u.Query()
 q.Set("client_id",row.ClientID);q.Set("redirect_uri",callback);q.Set("response_type","code");q.Set("scope",scope);q.Set("state",state)
 if row.Provider=="google"{q.Set("access_type","offline");q.Set("prompt","consent")}
 u.RawQuery=q.Encode()
 writeJSON(w,200,map[string]string{"authorization_url":u.String(),"redirect_uri":callback})
}
func (a *App) directCalendarOAuthCallback(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 state:=r.URL.Query().Get("state");code:=r.URL.Query().Get("code")
 if len(state)!=64||len(code)==0||len(code)>2048||r.URL.Query().Get("error")!=""{
  http.Error(w,"OAuth blev afbrudt eller er ugyldig",400);return
 }
 id:=a.sessionID(r)
 if id==""{http.Error(w,"Log ind i FamilieHub først",401);return}
 user,ok:=a.accountIdentity(r)
 if !ok||!user.Admin{http.Error(w,"Kun administrator kan forbinde en kalender",403);return}
 rows,err:=a.db.Query("SELECT calendar_id,session_hash,expires_at FROM direct_oauth_states WHERE state_hash=?",sha(state))
 if err!=nil||len(rows)!=1{http.Error(w,"Ugyldig eller udløbet OAuth-forbindelse",403);return}
 v:=rows[0]
 // Single use. Consume before exchanging the code.
 _=a.db.Exec("DELETE FROM direct_oauth_states WHERE state_hash=?",sha(state))
 if v["session_hash"]!=sha(id){http.Error(w,"OAuth startede i en anden session",403);return}
 expiry,err:=time.Parse(time.RFC3339,v["expires_at"])
 if err!=nil||time.Now().After(expiry){http.Error(w,"OAuth-forbindelsen er udløbet",403);return}
 row,err:=a.directCalendarByID(v["calendar_id"])
 if err!=nil{http.Error(w,"Kalenderforbindelsen findes ikke",404);return}
 _,tokenURL,_:=oauthEndpoints(row.Provider)
 secret,err:=a.decryptAIKey(row.Secret)
 if err!=nil||tokenURL==""{http.Error(w,"Ugyldig kalenderkonfiguration",400);return}
 values:=url.Values{"grant_type":{"authorization_code"},"code":{code},"client_id":{row.ClientID},"client_secret":{secret},"redirect_uri":{a.oauthRedirect()}}
 access,refresh,err:=a.directOAuthExchange(r.Context(),tokenURL,values)
 if err!=nil||access==""||refresh==""{http.Error(w,"Kalenderen kunne ikke forbindes. Prøv at logge ind igen.",502);return}
 encryptedAccess,e1:=a.encryptAIKey(access);encryptedRefresh,e2:=a.encryptAIKey(refresh)
 if e1!=nil||e2!=nil{http.Error(w,"Kunne ikke gemme adgang",500);return}
 if err:=a.db.Exec("UPDATE direct_calendars SET access_cipher=?,refresh_cipher=? WHERE id=?",encryptedAccess,encryptedRefresh,row.ID);err!=nil{
  http.Error(w,"Kunne ikke gemme forbindelsen",500);return
 }
 a.broadcast()
 http.Redirect(w,r,"/#indstillinger",http.StatusSeeOther)
}
func (a *App) directOAuthExchange(ctx context.Context,endpoint string,values url.Values)(string,string,error){
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,endpoint,strings.NewReader(values.Encode()))
 if err!=nil{return "","",err}
 req.Header.Set("Content-Type","application/x-www-form-urlencoded")
 client:=a.client;if client==nil{client=&http.Client{Timeout:10*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}}
 resp,err:=client.Do(req);if err!=nil{return "","",err}
 defer resp.Body.Close()
 if resp.StatusCode!=200{return "","",fmt.Errorf("oauth token status %d",resp.StatusCode)}
 raw,err:=io.ReadAll(io.LimitReader(resp.Body,64<<10));if err!=nil{return "","",err}
 var t struct {AccessToken string `json:"access_token"`;RefreshToken string `json:"refresh_token"`}
 if err=json.Unmarshal(raw,&t);err!=nil{return "","",err}
 return t.AccessToken,t.RefreshToken,nil
}
func (a *App) directAccessToken(ctx context.Context,row calendarRecord)(string,error){
 // Access tokens are refreshable, and refresh tokens are encrypted in /data.
 // Refresh on each calendar read rather than keeping unbounded stale tokens.
 secret,err:=a.decryptAIKey(row.Secret);if err!=nil{return "",errors.New("Kalendernøgle kan ikke læses")}
 refresh,err:=a.decryptAIKey(row.Refresh);if err!=nil||refresh==""{return "",errors.New("Forbind kalenderen med OAuth")}
 _,tokenURL,_:=oauthEndpoints(row.Provider)
 if tokenURL==""{return "",errors.New("Ukendt OAuth-udbyder")}
 values:=url.Values{"grant_type":{"refresh_token"},"refresh_token":{refresh},"client_id":{row.ClientID},"client_secret":{secret}}
 access,newRefresh,err:=a.directOAuthExchange(ctx,tokenURL,values)
 if err!=nil{return "",errors.New("OAuth-token kunne ikke fornyes")}
 if access==""{return "",errors.New("OAuth returnerede ikke adgangstoken")}
 encryptedAccess,e:=a.encryptAIKey(access);if e!=nil{return "",e}
 encryptedRefresh:=row.Refresh
 if newRefresh!=""{encryptedRefresh,e=a.encryptAIKey(newRefresh);if e!=nil{return "",e}}
 if err:=a.db.Exec("UPDATE direct_calendars SET access_cipher=?,refresh_cipher=? WHERE id=?",encryptedAccess,encryptedRefresh,row.ID);err!=nil{return "",err}
 return access,nil
}

func (a *App) directCalendarTest(w http.ResponseWriter,r *http.Request){
 row,err:=a.directCalendarByID(r.PathValue("id"))
 if err!=nil{apiError(w,404,"Kalenderen findes ikke");return}
 now:=time.Now().In(a.loc)
 ctx,cancel:=context.WithTimeout(r.Context(),10*time.Second)
 defer cancel()
 ev,err:=a.fetchDirectCalendar(ctx,row,now,now.AddDate(0,0,7))
 if err!=nil{apiError(w,502,"Forbindelsen fejlede. Kontrollér kalenderadresse, tilladelser eller OAuth-login.");return}
 writeJSON(w,200,map[string]any{"ok":true,"count":len(ev)})
}
