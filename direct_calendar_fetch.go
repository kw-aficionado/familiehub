package main

import (
 "context"
 "encoding/json"
 "encoding/xml"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "strings"
 "time"
)

func externalEvent(row calendarRecord,id,title,begin,end,location,notes string) Event {
 return Event{ID:"direct:"+row.ID+":"+sha(id+begin)[:16],Title:title,Start:begin,End:end,Kind:"kalender",External:true,SourceCalendar:"direct:"+row.ID,PersonID:row.PersonID,PersonIDs:func()[]string{if row.PersonID!=""{return []string{row.PersonID}};return nil}(),Location:location,Notes:notes}
}
func (a *App) directEvents(ctx context.Context,from,to time.Time,people []Person)([]Event,[]string){
 out:=[]Event{}
 warnings:=[]string{}
 rows,err:=a.calendarRecords()
 if err!=nil{return out,[]string{"Kalenderforbindelser kan ikke læses"}}
 for _,row:=range rows {
  events,e:=a.fetchDirectCalendar(ctx,row,from,to)
  if e!=nil {warnings=append(warnings,row.Name+": kalenderen kunne ikke hentes");continue}
  for _,ev:=range events {
   ev=decorateExternalEvent(ev,people)
   if len(ev.PersonIDs)==0 {
    ev.PersonID=row.PersonID
    if row.PersonID!=""{ev.PersonIDs=[]string{row.PersonID}}
   }
   out=append(out,ev)
  }
 }
 return out,warnings
}
func (a *App) fetchDirectCalendar(ctx context.Context,row calendarRecord,from,to time.Time)([]Event,error){
 switch row.Provider{
 case "google":return a.googleCalendarEvents(ctx,row,from,to)
 case "microsoft":return a.microsoftCalendarEvents(ctx,row,from,to)
 case "ics","icloud":return a.icsCalendarEvents(ctx,row,from,to)
 }
 return nil,errors.New("unsupported calendar provider")
}
func (a *App) calendarFetch(ctx context.Context,method,endpoint,token,user,password,contentType,body string)([]byte,error){
 req,err:=http.NewRequestWithContext(ctx,method,endpoint,strings.NewReader(body))
 if err!=nil{return nil,err}
 if token!=""{req.Header.Set("Authorization","Bearer "+token)}
 if user!=""{req.SetBasicAuth(user,password)}
 if contentType!=""{req.Header.Set("Content-Type",contentType)}
 if method=="REPORT"{req.Header.Set("Depth","1")}
 req.Header.Set("Prefer","outlook.timezone=\"UTC\", outlook.body-content-type=\"text\"")
 client:=a.client
 if client==nil{client=&http.Client{Timeout:10*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}}
 resp,err:=client.Do(req);if err!=nil{return nil,err}
 defer resp.Body.Close()
 if resp.StatusCode<200||resp.StatusCode>=300{return nil,fmt.Errorf("calendar returned HTTP %d",resp.StatusCode)}
 return io.ReadAll(io.LimitReader(resp.Body,4<<20))
}
func (a *App) googleCalendarEvents(ctx context.Context,row calendarRecord,from,to time.Time)([]Event,error){
 token,err:=a.directAccessToken(ctx,row);if err!=nil{return nil,err}
 out:=[]Event{}
 page:=""
 for i:=0;i<5;i++{
  q:=url.Values{"timeMin":{from.UTC().Format(time.RFC3339)},"timeMax":{to.UTC().Format(time.RFC3339)},"singleEvents":{"true"},"maxResults":{"250"},"orderBy":{"startTime"}}
  if page!=""{q.Set("pageToken",page)}
  endpoint:="https://www.googleapis.com/calendar/v3/calendars/primary/events?"+q.Encode()
  raw,err:=a.calendarFetch(ctx,"GET",endpoint,token,"","","","")
  if err!=nil{return out,err}
  var data struct{
   NextPageToken string `json:"nextPageToken"`
   Items []struct{
    ID string `json:"id"`
    Summary string `json:"summary"`
    Status string `json:"status"`
    Description string `json:"description"`
    Location string `json:"location"`
    Start struct{DateTime string `json:"dateTime"`;Date string `json:"date"`} `json:"start"`
    End struct{DateTime string `json:"dateTime"`;Date string `json:"date"`} `json:"end"`
   } `json:"items"`
  }
  if err=json.Unmarshal(raw,&data);err!=nil{return out,err}
  for _,v:=range data.Items{
   if v.Status=="cancelled"{continue}
   st:=v.Start.DateTime;en:=v.End.DateTime
   if st==""{st=toISO(v.Start.Date,a.loc)}
   if en==""{en=toISO(v.End.Date,a.loc)}
   if st==""||en==""{continue}
   out=append(out,externalEvent(row,v.ID,v.Summary,st,en,v.Location,v.Description))
  }
  if data.NextPageToken==""{return out,nil}
  page=data.NextPageToken
 }
 return out,errors.New("calendar has too many pages")
}
func (a *App) microsoftCalendarEvents(ctx context.Context,row calendarRecord,from,to time.Time)([]Event,error){
 token,err:=a.directAccessToken(ctx,row);if err!=nil{return nil,err}
 q:=url.Values{"startDateTime":{from.UTC().Format(time.RFC3339)},"endDateTime":{to.UTC().Format(time.RFC3339)},"$top":{"200"}}
 endpoint:="https://graph.microsoft.com/v1.0/me/calendarView?"+q.Encode()
 out:=[]Event{}
 for page:=0;page<5;page++{
  raw,err:=a.calendarFetch(ctx,"GET",endpoint,token,"","","","")
  if err!=nil{return out,err}
  var data struct {
   Next string `json:"@odata.nextLink"`
   Value []struct {
    ID string `json:"id"`
    Subject string `json:"subject"`
    IsCancelled bool `json:"isCancelled"`
    BodyPreview string `json:"bodyPreview"`
    Location struct{DisplayName string `json:"displayName"`} `json:"location"`
    Start struct{DateTime string `json:"dateTime"`} `json:"start"`
    End struct{DateTime string `json:"dateTime"`} `json:"end"`
   } `json:"value"`
  }
  if err=json.Unmarshal(raw,&data);err!=nil{return out,err}
  for _,v:=range data.Value {
   if v.IsCancelled{continue}
   st:=graphDateTime(v.Start.DateTime);en:=graphDateTime(v.End.DateTime)
   if st==""||en==""{continue}
   out=append(out,externalEvent(row,v.ID,v.Subject,st,en,v.Location.DisplayName,v.BodyPreview))
  }
  if data.Next==""{return out,nil}
  next,err:=url.Parse(data.Next)
  if err!=nil||next.Scheme!="https"||next.Host!="graph.microsoft.com"||!strings.HasPrefix(next.Path,"/v1.0/me/"){return out,errors.New("unexpected Graph paging URL")}
  endpoint=data.Next
 }
 return out,errors.New("calendar has too many pages")
}
func graphDateTime(s string)string {
 if s==""{return ""}
 for _,layout:=range []string{time.RFC3339Nano,"2006-01-02T15:04:05.9999999","2006-01-02T15:04:05","2006-01-02T15:04"}{
  if t,e:=time.Parse(layout,s);e==nil{return t.UTC().Format(time.RFC3339)}
 }
 return ""
}
type calDAVMultiStatus struct{
 Responses []struct {
  Props []struct {Data string `xml:"calendar-data"`} `xml:"propstat>prop"`
 } `xml:"response"`
}
func (a *App) icsCalendarEvents(ctx context.Context,row calendarRecord,from,to time.Time)([]Event,error){
 if !validCalendarEndpoint(row.URL){return nil,errors.New("invalid calendar URL")}
 if row.Provider=="icloud"{
  pass,e:=a.decryptAIKey(row.Secret);if e!=nil{return nil,e}
  body:=fmt.Sprintf("<?xml version=\"1.0\"?><c:calendar-query xmlns:d=\"DAV:\" xmlns:c=\"urn:ietf:params:xml:ns:caldav\"><d:prop><c:calendar-data/></d:prop><c:filter><c:comp-filter name=\"VCALENDAR\"><c:comp-filter name=\"VEVENT\"><c:time-range start=\"%s\" end=\"%s\"/></c:comp-filter></c:comp-filter></c:filter></c:calendar-query>",from.UTC().Format("20060102T150405Z"),to.UTC().Format("20060102T150405Z"))
  raw,err:=a.calendarFetch(ctx,"REPORT",row.URL,"",row.Username,pass,"application/xml; charset=utf-8",body)
  if err!=nil{return nil,err}
  var dav calDAVMultiStatus
  if err=xml.Unmarshal(raw,&dav);err!=nil{return nil,err}
  out:=[]Event{}
  for _,response:=range dav.Responses{for _,p:=range response.Props{
   out=append(out,parseICS(p.Data,row,from,to,a.loc)...)
  }}
  return out,nil
 }
 raw,err:=a.calendarFetch(ctx,"GET",row.URL,"","","","","")
 if err!=nil{return nil,err}
 return parseICS(string(raw),row,from,to,a.loc),nil
}
type icsItem struct{Props map[string]string;StartKey,EndKey string}
func parseICS(raw string,row calendarRecord,from,to time.Time,loc *time.Location)[]Event {
 raw=strings.ReplaceAll(strings.ReplaceAll(raw,"\r\n","\n"),"\r","\n")
 var lines []string
 for _,line:=range strings.Split(raw,"\n"){
  if len(line)>0&&(line[0]==' '||line[0]=='\t')&&len(lines)>0{lines[len(lines)-1]+=line[1:]}else{lines=append(lines,line)}
 }
 items:=[]icsItem{}
 var item icsItem
 inside:=false
 for _,line:=range lines{
  if line=="BEGIN:VEVENT"{inside=true;item=icsItem{Props:map[string]string{}};continue}
  if line=="END:VEVENT"{if inside{items=append(items,item)};inside=false;continue}
  if !inside{continue}
  key,val,ok:=strings.Cut(line,":");if !ok{continue}
  typ:=strings.ToUpper(strings.Split(key,";")[0])
  if typ=="DTSTART"{item.StartKey=key}
  if typ=="DTEND"{item.EndKey=key}
  item.Props[typ]=val
 }
 out:=[]Event{}
 for _,v:=range items{
  start,ok:=icsDate(v.Props["DTSTART"],v.StartKey,loc);if !ok{continue}
  end,ok:=icsDate(v.Props["DTEND"],v.EndKey,loc)
  if !ok||!end.After(start){end=start.Add(time.Hour);if len(v.Props["DTSTART"])==8{end=start.AddDate(0,0,1)}}
  if !end.After(from)||!start.Before(to){continue}
  title:=icsUnescape(v.Props["SUMMARY"]);if title==""{title="Kalenderaftale"}
  uid:=v.Props["UID"];if uid==""{uid=sha(title+start.String())}
  out=append(out,externalEvent(row,uid,title,start.Format(time.RFC3339),end.Format(time.RFC3339),icsUnescape(v.Props["LOCATION"]),icsUnescape(v.Props["DESCRIPTION"])))
 }
 return out
}
func icsDate(value,key string,loc *time.Location)(time.Time,bool){
 if len(value)==8{
  t,e:=time.ParseInLocation("20060102",value,loc);return t,e==nil
 }
 if strings.HasSuffix(value,"Z"){
  t,e:=time.Parse("20060102T150405Z",value);return t,e==nil
 }
 tz:=loc
 for _,segment:=range strings.Split(key,";"){
  if strings.HasPrefix(strings.ToUpper(segment),"TZID="){
   if l,e:=time.LoadLocation(strings.TrimPrefix(segment,"TZID="));e==nil{tz=l}
  }
 }
 t,e:=time.ParseInLocation("20060102T150405",value,tz)
 if e!=nil{t,e=time.ParseInLocation("20060102T1504",value,tz)}
 return t,e==nil
}
func icsUnescape(s string)string{
 r:=strings.NewReplacer("\\n","\n","\\N","\n","\\,",",","\\;",";","\\\\","\\")
 return r.Replace(s)
}
