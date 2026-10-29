/* Notifications only. No service-worker caching, so FamiliaHub updates remain fresh. */
self.addEventListener('push',function(event){
  var data={};
  try{data=event.data?event.data.json():{}}catch(e){data={body:'Du har en påmindelse fra FamilieHub.'}}
  var title=typeof data.title==='string'?data.title:'FamilieHub';
  var body=typeof data.body==='string'?data.body:'Du har nye opgaver.';
  var target=typeof data.url==='string'&&data.url.startsWith('/#')?data.url:'/#opgaver';
  event.waitUntil(self.registration.showNotification(title,{
    body:body,icon:'/icon.svg',badge:'/icon.svg',tag:'familiehub-task-digest',
    data:{url:target},renotify:false
  }));
});
self.addEventListener('notificationclick',function(event){
  event.notification.close();
  var url=event.notification.data&&event.notification.data.url||'/#opgaver';
  event.waitUntil((async function(){
    var windows=await clients.matchAll({type:'window',includeUncontrolled:true});
    for(var i=0;i<windows.length;i++){
      if(windows[i].url.includes(self.location.origin)){await windows[i].focus();if(windows[i].navigate)await windows[i].navigate(url);return}
    }
    if(clients.openWindow)await clients.openWindow(url);
  })());
});
