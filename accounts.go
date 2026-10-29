package main

import (
 "crypto/hmac"
 "crypto/rand"
 "crypto/sha256"
 "encoding/hex"
 "fmt"
 "net/http"
 "regexp"
 "strconv"
 "strings"
 "time"
)

type Account struct {
 ID string `json:"id"`
 Username string `json:"username"`
 PersonID string `json:"person_id"`
 Enabled bool `json:"enabled"`
}

type identity struct {
 AccountID string
 Username string
 PersonID string
 Admin bool
}

var accountUsernamePattern=regexp.MustCompile(`^[a-z][a-z0-9._-]{2,31}$`)
const passwordIterations=600000

func validAccountPassword(p string) bool {
 return len(p)>=12 && len(p)<=128 && !strings.ContainsRune(p,0)
}

// PBKDF2-HMAC-SHA256, one 32-byte block with a random 16-byte salt.
// Store only salt and derived key, never a readable password.
func passwordKDF(password string,salt []byte,rounds int) []byte {
 block:=make([]byte,len(salt)+4)
 copy(block,salt)
 block[len(salt)+3]=1
 h:=hmac.New(sha256.New,[]byte(password))
 h.Write(block)
 prev:=h.Sum(nil)
 sum:=append([]byte(nil),prev...)
 for i:=1;i<rounds;i++ {
  h.Reset()
  h.Write(prev)
  prev=h.Sum(prev[:0])
  for k:=range sum {sum[k]^=prev[k]}
 }
 return sum
}
func hashAccountPassword(password string)(string,error) {
 salt:=make([]byte,16)
 if _,err:=rand.Read(salt);err!=nil{return "",err}
 dk:=passwordKDF(password,salt,passwordIterations)
 return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",passwordIterations,hex.EncodeToString(salt),hex.EncodeToString(dk)),nil
}
func checkAccountPassword(password,stored string) bool {
 parts:=strings.Split(stored,"$")
 if len(parts)!=4||parts[0]!="pbkdf2-sha256"{return false}
 rounds,err:=strconv.Atoi(parts[1]);if err!=nil||rounds<200000||rounds>1000000{return false}
 salt,e1:=hex.DecodeString(parts[2]);digest,e2:=hex.DecodeString(parts[3])
 if e1!=nil||e2!=nil||len(salt)!=16||len(digest)!=32{return false}
 computed:=passwordKDF(password,salt,rounds)
 return hmac.Equal(computed,digest)
}
func (a *App) accountIdentity(r *http.Request)(identity,bool) {
 sid:=a.sessionID(r)
 if sid==""{return identity{},false}
 rows,err:=a.db.Query("SELECT account_id FROM sessions WHERE id=?",sha(sid))
 if err!=nil||len(rows)==0{return identity{},false}
 id:=rows[0]["account_id"]
 if id=="" {return identity{Username:"Administrator",Admin:true},true}
 accounts,err:=a.db.Query("SELECT username,person_id,enabled FROM accounts WHERE id=?",id)
 if err!=nil||len(accounts)==0||accounts[0]["enabled"]!="1"{return identity{},false}
 return identity{AccountID:id,Username:accounts[0]["username"],PersonID:accounts[0]["person_id"],Admin:false},true
}
func identityResponse(id identity)map[string]any {
 return map[string]any{"username":id.Username,"person_id":id.PersonID,"role":func()string{if id.Admin{return "admin"};return "member"}()}
}
func (a *App) withAdmin(h http.HandlerFunc)http.HandlerFunc {
 return a.withAuth(func(w http.ResponseWriter,r *http.Request){
  account,ok:=a.accountIdentity(r)
  if !ok||!account.Admin{apiError(w,http.StatusForbidden,"Kun administratoren kan administrere brugere");return}
  h(w,r)
 })
}
func (a *App) accountsList(w http.ResponseWriter,r *http.Request){
 rows,err:=a.db.Query("SELECT id,username,person_id,enabled FROM accounts ORDER BY username")
 if err!=nil{apiError(w,500,"Kunne ikke hente brugere");return}
 out:=[]Account{}
 for _,row:=range rows{out=append(out,Account{ID:row["id"],Username:row["username"],PersonID:row["person_id"],Enabled:row["enabled"]=="1"})}
 writeJSON(w,200,out)
}
func (a *App) accountSave(w http.ResponseWriter,r *http.Request){
 var v struct{
  Username string `json:"username"`
  PersonID string `json:"person_id"`
  Password string `json:"password"`
  Enabled bool `json:"enabled"`
 }
 if !readJSON(w,r,&v){return}
 v.Username=strings.ToLower(strings.TrimSpace(v.Username))
 if !accountUsernamePattern.MatchString(v.Username)||v.Username=="admin"{
  apiError(w,400,"Brugernavn skal have 3–32 tegn (a-z, 0-9, punktum, bindestreg eller _), og kan ikke være admin");return
 }
 if v.PersonID!=""{
  found,err:=a.db.Query("SELECT id FROM people WHERE id=?",v.PersonID)
  if err!=nil{apiError(w,500,"Kunne ikke kontrollere familiemedlem");return}
  if len(found)==0{apiError(w,400,"Familiemedlemmet findes ikke");return}
 }
 enabled:="0";if v.Enabled{enabled="1"}
 if r.Method==http.MethodPost {
  if !validAccountPassword(v.Password){apiError(w,400,"Adgangskoden skal være mellem 12 og 128 tegn");return}
  key,err:=hashAccountPassword(v.Password)
  if err!=nil{apiError(w,500,"Adgangskoden kunne ikke oprettes");return}
  id:=newID()
  if err:=a.db.Exec("INSERT INTO accounts(id,username,password_hash,person_id,enabled) VALUES(?,?,?,?,?)",id,v.Username,key,v.PersonID,enabled);err!=nil{
   apiError(w,409,"Brugernavnet er allerede i brug");return
  }
  a.broadcast()
  writeJSON(w,200,Account{ID:id,Username:v.Username,PersonID:v.PersonID,Enabled:v.Enabled})
  return
 }
 if v.Password!=""{apiError(w,400,"Brug Nulstil adgangskode for at ændre adgangskoden");return}
 id:=r.PathValue("id")
 if !validID(id){apiError(w,400,"Ugyldigt bruger-ID");return}
 rows,err:=a.db.Query("SELECT id FROM accounts WHERE id=?",id)
 if err!=nil{apiError(w,500,"Kunne ikke kontrollere bruger");return}
 if len(rows)==0{apiError(w,404,"Brugeren findes ikke");return}
 if err:=a.db.Exec("UPDATE accounts SET username=?,person_id=?,enabled=? WHERE id=?",v.Username,v.PersonID,enabled,id);err!=nil{
  apiError(w,409,"Brugernavnet er allerede i brug");return
 }
 if !v.Enabled {_=a.db.Exec("DELETE FROM sessions WHERE account_id=?",id)}
 a.broadcast()
 writeJSON(w,200,Account{ID:id,Username:v.Username,PersonID:v.PersonID,Enabled:v.Enabled})
}
func (a *App) accountResetPassword(w http.ResponseWriter,r *http.Request){
 id:=r.PathValue("id")
 if !validID(id){apiError(w,400,"Ugyldigt bruger-ID");return}
 var v struct{Password string `json:"password"`}
 if !readJSON(w,r,&v){return}
 if !validAccountPassword(v.Password){apiError(w,400,"Adgangskoden skal være mellem 12 og 128 tegn");return}
 exists,err:=a.db.Query("SELECT id FROM accounts WHERE id=?",id)
 if err!=nil{apiError(w,500,"Kunne ikke kontrollere bruger");return}
 if len(exists)==0{apiError(w,404,"Brugeren findes ikke");return}
 key,err:=hashAccountPassword(v.Password)
 if err!=nil{apiError(w,500,"Kunne ikke gemme adgangskode");return}
 if err:=a.db.Exec("UPDATE accounts SET password_hash=? WHERE id=?",key,id);err!=nil{apiError(w,500,"Kunne ikke gemme adgangskode");return}
 _=a.db.Exec("DELETE FROM sessions WHERE account_id=?",id)
 writeJSON(w,200,map[string]bool{"ok":true})
}
func (a *App) accountDelete(w http.ResponseWriter,r *http.Request){
 id:=r.PathValue("id")
 if !validID(id){apiError(w,400,"Ugyldigt bruger-ID");return}
 rows,err:=a.db.Query("SELECT id FROM accounts WHERE id=?",id)
 if err!=nil{apiError(w,500,"Kunne ikke kontrollere bruger");return}
 if len(rows)==0{apiError(w,404,"Brugeren findes ikke");return}
 _=a.db.Exec("DELETE FROM sessions WHERE account_id=?",id)
 if err:=a.db.Exec("DELETE FROM accounts WHERE id=?",id);err!=nil{apiError(w,500,"Brugeren kunne ikke slettes");return}
 a.broadcast()
 writeJSON(w,200,map[string]bool{"ok":true})
}
func (a *App) accountMe(w http.ResponseWriter,r *http.Request){
 user,ok:=a.accountIdentity(r)
 if !ok{apiError(w,401,"Log ind igen");return}
 writeJSON(w,200,identityResponse(user))
}
func (a *App) accountChangePassword(w http.ResponseWriter,r *http.Request){
 user,ok:=a.accountIdentity(r)
 if !ok{apiError(w,401,"Log ind igen");return}
 if user.Admin{apiError(w,400,"Administratorens adgangskode ændres i APP_PASSWORD under Yggdrasil");return}
 var v struct{
  OldPassword string `json:"old_password"`
  NewPassword string `json:"new_password"`
 }
 if !readJSON(w,r,&v){return}
 if !validAccountPassword(v.NewPassword){apiError(w,400,"Ny adgangskode skal være mellem 12 og 128 tegn");return}
 accounts,err:=a.db.Query("SELECT password_hash FROM accounts WHERE id=?",user.AccountID)
 if err!=nil||len(accounts)==0{apiError(w,401,"Brugeren findes ikke");return}
 if !checkAccountPassword(v.OldPassword,accounts[0]["password_hash"]){apiError(w,401,"Den nuværende adgangskode er forkert");return}
 key,err:=hashAccountPassword(v.NewPassword)
 if err!=nil{apiError(w,500,"Kunne ikke gemme adgangskode");return}
 if err:=a.db.Exec("UPDATE accounts SET password_hash=? WHERE id=?",key,user.AccountID);err!=nil{apiError(w,500,"Kunne ikke gemme adgangskode");return}
 _=a.db.Exec("DELETE FROM sessions WHERE account_id=?",user.AccountID)
 a.cookie(w,"",time.Unix(0,0))
 writeJSON(w,200,map[string]bool{"ok":true})
}
