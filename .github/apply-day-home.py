from pathlib import Path


def change(path, pairs):
    p = Path(path)
    text = p.read_text(encoding='utf-8')
    for before, after in pairs:
        count = text.count(before)
        if count != 1:
            raise RuntimeError(f'{path}: expected one source anchor, got {count}: {before[:100]}')
        text = text.replace(before, after, 1)
    p.write_text(text, encoding='utf-8')


change('web/app.js', [
    ("view:'week', focus:new Date()", "view:'day', focus:familyToday()"),
    ("taskPeriod:'week', taskFocus:new Date(), taskFilter:'', taskStatus:'open'", "taskPeriod:'today', taskFocus:familyToday(), taskFilter:'', taskStatus:'all'"),
    ("todayDate:'', loading:0, socket:null", "todayDate:'', loading:0, taskLoading:0, socket:null"),
    ("  function pad(n)", "  // Use the family's Copenhagen date, also when a device is in another timezone.\n  function familyToday(){return new Date(window.FamilyHubPlannerDates.today()+'T12:00:00')}\n  function pad(n)"),
    ("  function setView(v){S.view=v;$('dayBtn').classList.toggle('active',v==='day');$('weekBtn').classList.toggle('active',v==='week');load()}", """  function updateCalendarViewButtons(){
    ['day','week'].forEach(function(view){
      var button=$(view+'Btn'),active=S.view===view;
      button.classList.toggle('active',active);
      button.setAttribute('aria-pressed',String(active));
    });
  }
  function setView(v){S.view=v;updateCalendarViewButtons();load()}"""),
    ("  function render(){\n    var r=range();", "  function render(){\n    updateCalendarViewButtons();\n    var r=range();"),
    ("  function renderTasks(){var root=$('taskList');root.replaceChildren();var tasks=S.tasks.filter(function(t){return !S.filter||t.assigned_to===S.filter}).sort(function(a,b){return Number(a.done)-Number(b.done)}).slice(0,4);if(!tasks.length)", """  function renderTasks(){
    var root=$('taskList');root.replaceChildren();
    var tasks=S.tasks.filter(function(t){return !S.filter||t.assigned_to===S.filter}).sort(function(a,b){return Number(a.done)-Number(b.done)});
    var today=dayString(S.focus)===window.FamilyHubPlannerDates.today();
    $('showDayTasksBtn').textContent=today?'Se alle dagens opgaver':'Se alle opgaver denne dag';
    $('showDayTasksBtn').setAttribute('aria-label','Se alle opgaver for '+formatDate(S.focus,{weekday:'long',day:'numeric',month:'long'}));
    $('taskPreviewCount').textContent=tasks.length>4?'Viser 4 af '+tasks.length+' opgaver.':'';
    tasks=tasks.slice(0,4);
    if(!tasks.length)"""),
    ("  function taskWeekStart(){return dayString(startWeek(S.taskFocus))}", """  // Open the complete checklist for the day shown on the home page, not every task series.
  function openDayTasks(){
    S.taskPeriod='today';
    S.taskFocus=new Date(S.focus.getTime());
    S.taskFilter=S.filter;
    S.taskStatus='all';
    if(location.hash==='#opgaver')syncPage();else location.hash='#opgaver';
  }
  function selectTaskPeriod(period){
    if(['today','week','all'].indexOf(period)===-1)return Promise.resolve();
    S.taskPeriod=period;
    if(period==='today')S.taskFocus=familyToday();
    S.taskStatus=period==='today'?'all':'open';
    return loadTasksPage();
  }
  function taskWeekStart(){return dayString(startWeek(S.taskFocus))}"""),
    ("""  async function loadTasksPage(){
    if(S.page!=='opgaver')return;
    var data=await Promise.all([
      api('/api/tasks/week?start='+encodeURIComponent(taskWeekStart())),
      api('/api/tasks/all')
    ]);
    S.weekEntries=data[0].entries||[];
    S.allTasks=data[1]||[];
    renderTasksPage();
  }""", """  async function loadTasksPage(){
    if(S.page!=='opgaver')return;
    var token=++S.taskLoading,all=S.taskPeriod==='all';
    var data=await api(all?'/api/tasks/all':'/api/tasks/week?start='+encodeURIComponent(taskWeekStart()));
    // A slow response from another day/period must not replace the current checklist.
    if(token!==S.taskLoading||S.page!=='opgaver')return;
    if(all)S.allTasks=data||[];else S.weekEntries=data.entries||[];
    renderTasksPage();
  }"""),
    ("      'Opgaver '+formatDate(S.taskFocus,{day:'numeric',month:'long'}):", "      (dayString(S.taskFocus)===window.FamilyHubPlannerDates.today()?'Dagens opgaver · ':'Opgaver ')+formatDate(S.taskFocus,{day:'numeric',month:'long'}):"),
    ("$('todayBtn').addEventListener('click',function(){S.focus=new Date();load()});", "$('todayBtn').addEventListener('click',function(){S.focus=familyToday();load()});"),
    ("  $('tasksAddBtn').addEventListener('click',function(){openTask()});", "  $('showDayTasksBtn').addEventListener('click',openDayTasks);\n  $('tasksAddBtn').addEventListener('click',function(){openTask()});"),
    ("  document.querySelectorAll('[data-period]').forEach(function(button){button.addEventListener('click',function(){S.taskPeriod=button.getAttribute('data-period');S.taskStatus='open';loadTasksPage()})});", "  document.querySelectorAll('[data-period]').forEach(function(button){button.addEventListener('click',function(){selectTaskPeriod(button.getAttribute('data-period')).catch(function(err){$('tasksPageList').replaceChildren(el('p',{'class':'error'},err.message))})})});"),
    ("  $('tasksCurrentBtn').addEventListener('click',function(){S.taskFocus=new Date();loadTasksPage()});", "  $('tasksCurrentBtn').addEventListener('click',function(){selectTaskPeriod('today').catch(function(err){$('tasksPageList').replaceChildren(el('p',{'class':'error'},err.message))})});"),
])

change('web/index.html', [
    ('/app.js?v=planner-start-date-1', '/app.js?v=day-home-1'),
    ('/style.css?v=direct-calendar-push-1', '/style.css?v=day-home-1'),
    ('<button id="dayBtn" class="segment">Dag</button><button id="weekBtn" class="segment active">Uge</button>', '<button id="dayBtn" class="segment active" aria-pressed="true">Dag</button><button id="weekBtn" class="segment" aria-pressed="false">Uge</button>'),
    ('<a class="text-button" href="#opgaver">Se alle →</a>', '<button type="button" class="text-button" id="showDayTasksBtn">Se alle dagens opgaver</button>'),
    ('<div id="taskList"></div></article>', '<div id="taskList"></div><p id="taskPreviewCount" class="muted" aria-live="polite"></p></article>'),
    ('<button type="button" class="task-period" data-period="today">I dag</button><button type="button" class="task-period active" data-period="week">Denne uge</button>', '<button type="button" class="task-period active" data-period="today">Dagens opgaver</button><button type="button" class="task-period" data-period="week">Denne uge</button>'),
    ('<h2 id="tasksPeriodTitle">Denne uge</h2>', '<h2 id="tasksPeriodTitle">Dagens opgaver</h2>'),
])

css = Path('web/style.css')
css.write_text(css.read_text(encoding='utf-8') + '''
/* The complete daily checklist remains reachable on small tablet/phone screens. */
.tasks-panel .panel-title{flex-wrap:wrap;align-items:flex-start}
.tasks-panel .panel-title-actions{max-width:100%}
#showDayTasksBtn{white-space:normal;text-align:left}
#taskPreviewCount:not(:empty){padding-top:10px;font-size:12px}
''', encoding='utf-8')

readme = Path('README.md')
readme.write_text(readme.read_text(encoding='utf-8') + '''
## Dag som forside og alle dagens opgaver

Forsiden starter i **Dag**. **Uge** kan stadig vælges manuelt.
Opgavesiden starter i **Dagens opgaver**, med både udestående og udførte opgaver.
Forsidens **Se alle dagens opgaver** åbner den fulde tjekliste for den viste dag
uden begrænsningen på fire kort. Hvis en anden dato er valgt i kalenderen, følger
genvejen denne dato og hedder **Se alle opgaver denne dag**. Et valgt personfilter
på forsiden følger med; et gammelt statusfilter på opgavesiden gør ikke.
**Dagens opgaver** og datonavigationens **I dag** vender altid tilbage til den
aktuelle dato i Europe/Copenhagen. **Denne uge** og **Alle opgaver** er stadig
separate visninger. Ugeopgaver vises én gang i dagslisten, med de eksisterende
af hvorfor afkrydsninger og gentagelser. Ingen opgaver eller data ændres ved opdatering.
'''.replace('af hvorfor afkrydsninger', 'afkrydsninger'), encoding='utf-8')
