package main

import (
 "context"
 "crypto/tls"
 "errors"
 "fmt"
 "mime"
 "net"
 "net/mail"
 "net/smtp"
 "strconv"
 "strings"
 "time"
)

type EmailConfig struct {
 Host string `json:"host"`
 Port int `json:"port"`
 Username string `json:"username"`
 From string `json:"from"`
 SecretSet bool `json:"secret_set"`
 Recipients []EmailRecipient `json:"recipients"`
}
type EmailRecipient struct {
 PersonID string `json:"person_id"`
 Email string `json:"email"`
}
type smtpInternal struct {EmailConfig;Ciphertext string}
func (a *App) emailSettings()(smtpInternal,error){
 cfg:=smtpInternal{EmailConfig:EmailConfig{Port:587,Recipients:[]EmailRecipient{}}}
 rows,err:=a.db.Query("SELECT host,port,username,sender,password_cipher FROM smtp_settings WHERE id=1")
 if err!=nil{return cfg,err}
 if len(rows)>0{
  cfg.Host=rows[0]["host"];cfg.Port,_=strconv.Atoi(rows[0]["port"])
  cfg.Username=rows[0]["username"];cfg.From=rows[0]["sender"]
  cfg.Ciphertext=rows[0]["password_cipher"];cfg.SecretSet=cfg.Ciphertext!=""
 }
 recipients,err:=a.db.Query("SELECT person_id,email FROM smtp_recipients")
 if err!=nil{return cfg,err}
 for _,r:=range recipients{cfg.Recipients=append(cfg.Recipients,EmailRecipient{PersonID:r["person_id"],Email:r["email"]})}
 return cfg,nil
}
func validEmail(raw string)bool{
 if len(raw)<4||len(raw)>254||strings.ContainsAny(raw,"\r\n"){return false}
 addr,err:=mail.ParseAddress(raw)
 return err==nil&&addr.Address==raw
}
func validSMTPHost(host string)bool{
 if host==""||len(host)>253||strings.ContainsAny(host,":/@# \t\r\n"){return false}
 for _,part:=range strings.Split(host,"."){if part==""{return false}}
 return true
}
func (a *App) emailConfigGet(w http.ResponseWriter,r *http.Request){
 cfg,err:=a.emailSettings();if err!=nil{apiError(w,500,"E-mailindstillinger kunne ikke læses");return}
 writeJSON(w,200,cfg.EmailConfig)
}
func (a *App) emailConfigPut(w http.ResponseWriter,r *http.Request){
 var body struct{
  Host string `json:"host"`
  Port int `json:"port"`
  Username string `json:"username"`
  From string `json:"from"`
  Password string `json:"password"`
  ClearPassword bool `json:"clear_password"`
 }
 if !readJSON(w,r,&body){return}
 body.Host=strings.TrimSpace(body.Host);body.From=strings.TrimSpace(body.From)
 if body.Host!="" && (!validSMTPHost(body.Host)||body.Port<1||body.Port>65535||!validEmail(body.From)){apiError(w,400,"Ugyldig SMTP-vært, port eller afsenderadresse");return}
 if len(body.Username)>200||len(body.Password)>4096{apiError(w,400,"SMTP-oplysninger er for lange");return}
 old,err:=a.emailSettings();if err!=nil{apiError(w,500,"Kunne ikke læse SMTP-konfiguration");return}
 cipher:=old.Ciphertext;if body.ClearPassword{cipher=""}
 if body.Password!=""{
  cipher,err=a.encryptAIKey(body.Password);if err!=nil{apiError(w,500,"Kunne ikke beskytte SMTP-koden");return}
 }
 if err:=a.db.Exec("INSERT INTO smtp_settings(id,host,port,username,sender,password_cipher) VALUES(1,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET host=excluded.host,port=excluded.port,username=excluded.username,sender=excluded.sender,password_cipher=excluded.password_cipher",body.Host,strconv.Itoa(body.Port),body.Username,body.From,cipher);err!=nil{
  apiError(w,500,"Kunne ikke gemme SMTP-indstillinger");return
 }
 result,_:=a.emailSettings()
 writeJSON(w,200,result.EmailConfig)
}
func (a *App) emailRecipientPut(w http.ResponseWriter,r *http.Request){
 var v EmailRecipient
 if !readJSON(w,r,&v){return}
 if v.Email!=""&&!validEmail(v.Email){apiError(w,400,"E-mailadressen er ugyldig");return}
 valid:=false
 people,err:=a.people();if err!=nil{apiError(w,500,"Kunne ikke kontrollere modtager");return}
 for _,p:=range people{if p.ID==v.PersonID {valid=true;break}}
 if !valid{apiError(w,400,"Ukendt familiemedlem");return}
 if v.Email==""{
  err=a.db.Exec("DELETE FROM smtp_recipients WHERE person_id=?",v.PersonID)
 }else{
  err=a.db.Exec("INSERT INTO smtp_recipients(person_id,email) VALUES(?,?) ON CONFLICT(person_id) DO UPDATE SET email=excluded.email",v.PersonID,v.Email)
 }
 if err!=nil{apiError(w,500,"Modtageren kunne ikke gemmes");return}
 writeJSON(w,200,map[string]bool{"ok":true})
}
func (a *App) sendEmail(ctx context.Context,to,title,body string)error{
 cfg,err:=a.emailSettings();if err!=nil{return err}
 if cfg.Host==""||!validEmail(cfg.From)||!validEmail(to){return errors.New("SMTP og modtager skal konfigureres")}
 host:=cfg.Host
 address:=net.JoinHostPort(host,strconv.Itoa(cfg.Port))
 dialer:=&net.Dialer{Timeout:8*time.Second}
 conn,err:=dialer.DialContext(ctx,"tcp",address);if err!=nil{return errors.New("SMTP-server kunne ikke kontaktes")}
 // Implicit TLS on port 465; STARTTLS required on other ports for non-loopback.
 if cfg.Port==465{
  conn=tls.Client(conn,&tls.Config{ServerName:host,MinVersion:tls.VersionTLS12})
  if err=conn.(*tls.Conn).HandshakeContext(ctx);err!=nil{conn.Close();return errors.New("SMTP TLS-forbindelse fejlede")}
 }
 client,err:=smtp.NewClient(conn,host)
 if err!=nil{conn.Close();return errors.New("SMTP-forbindelse mislykkedes")}
 defer client.Close()
 loopback:=host=="localhost"||host=="127.0.0.1"||host=="::1"
 if cfg.Port!=465{
  if ok,_:=client.Extension("STARTTLS");ok{
   if err=client.StartTLS(&tls.Config{ServerName:host,MinVersion:tls.VersionTLS12});err!=nil{return errors.New("SMTP STARTTLS fejlede")}
  }else if !loopback{return errors.New("SMTP-serveren understøtter ikke STARTTLS")}
 }
 if cfg.Username!=""{
  password,e:=a.decryptAIKey(cfg.Ciphertext)
  if e!=nil{return errors.New("Kunne ikke læse SMTP-adgangskoden")}
  if err=client.Auth(smtp.PlainAuth("",cfg.Username,password,host));err!=nil{return errors.New("SMTP-login fejlede")}
 }
 if err=client.Mail(cfg.From);err!=nil{return errors.New("Afsender blev afvist")}
 if err=client.Rcpt(to);err!=nil{return errors.New("Modtager blev afvist")}
 writer,err:=client.Data();if err!=nil{return errors.New("SMTP afviste meddelelsen")}
 // Unicode-safe subject, encoded body. No user input may inject mail headers.
 subject:=mime.QEncoding.Encode("utf-8",strings.ReplaceAll(strings.ReplaceAll(title,"\r"," "),"\n"," "))
 msg:=fmt.Sprintf("From: <%s>\r\nTo: <%s>\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n",cfg.From,to,subject,body)
 if _,err=writer.Write([]byte(msg));err!=nil{writer.Close();return errors.New("SMTP kunne ikke sende")}
 if err=writer.Close();err!=nil{return errors.New("SMTP kunne ikke afslutte")}
 return client.Quit()
}
func (a *App) sendTaskNotification(ctx context.Context,p Person,title,msg string)error{
 var errs []error
 if p.NotifyService!=""&&a.cfg.HAURL!=""&&a.cfg.HAToken!="" {
  if err:=a.notify(ctx,p.NotifyService,title,msg);err==nil{return nil}else{errs=append(errs,err)}
 }
 cfg,err:=a.emailSettings()
 if err!=nil{return err}
 for _,r:=range cfg.Recipients {
  if r.PersonID==p.ID&&r.Email!=""&&cfg.Host!=""{
   if err:=a.sendEmail(ctx,r.Email,title,msg);err==nil{return nil}else{errs=append(errs,err)}
  }
 }
 if len(errs)>0{return errs[0]}
 return errors.New("Ingen notifikationskanal er konfigureret")
}
