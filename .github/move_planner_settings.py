from pathlib import Path
import re


def replace_once(text, before, after):
    assert text.count(before) == 1, 'Source anchor changed: ' + before[:100]
    return text.replace(before, after, 1)


html_path = Path('web/index.html')
html = html_path.read_text()
match = re.search(r'(?ms)^[ \t]*<section\b[^>]*\bid="plannerPanel"[^>]*>.*?</section>\n', html)
assert match and match.start() < html.index('<section id="settingsPage"'), 'Planner must currently be on the tasks page'
panel = match.group().strip()
panel = replace_once(panel, '<p id="plannerStatus" class="muted" role="status"></p>', '<p id="plannerStatus" class="muted" role="status"></p>\n<a id="plannerViewPlanLink" class="text-button hidden" href="#opgaver">Se den gemte opgaveplan →</a>')
html = html[:match.start()] + html[match.end():]
archive_end = '     <div id="settingsList" class="settings-list" aria-live="polite"></div>\n   </div>\n'
html = replace_once(html, archive_end, archive_end + '   ' + panel.replace('\n', '\n   ') + '\n')
assert html.count('id="plannerPanel"') == 1
html_path.write_text(html)

js_path = Path('web/app.js')
js = js_path.read_text()
helper = '''  // Keep the receipt beside the planner in Settings. Open the saved week only
  // when requested, without replacing the ordinary daily-task shortcut.
  function showSavedPlannerPlan(saved,draft){
    var startDate=saved.start_date||draft.start_date;
    closeModal();
    $('plannerStatus').textContent='Opgaveplan gemt: '+saved.created+' opgaver fra '+startDate+'.';
    var link=$('plannerViewPlanLink');
    link.onclick=function(){
      S.taskFocus=new Date(startDate+'T12:00:00');
      S.taskPeriod='week';
      S.taskFilter='';
      S.taskStatus='all';
      if(location.hash==='#opgaver')syncPage();
    };
    show('plannerViewPlanLink');
    $('plannerStatus').scrollIntoView({block:'nearest'});
  }
'''
js = replace_once(js, '  var plannerDraft=null;\n', '  var plannerDraft=null;\n' + helper)
start = js.index('          // A successful write must never be mistaken for failure if refreshing the list fails.\n')
end_marker = "          $('plannerStatus').scrollIntoView({block:'nearest'});\n"
end = js.index(end_marker, start) + len(end_marker)
old = js[start:end]
assert 'await loadTasksPage();' in old and 'var startDate=saved.start_date||draft.start_date;' in old
js = js[:start] + '          showSavedPlannerPlan(saved,draft);\n          await load();\n' + js[end:]
seed_start = js.index("  $('plannerSeedBtn').addEventListener('click',async function(){")
seed_end = js.index('  function openTemplate(t){', seed_start)
seed = js[seed_start:seed_end]
seed = replace_once(seed, "S.templates=await api('/api/templates')", 'await refreshSettingsData()')
js = js[:seed_start] + seed + js[seed_end:]
js_path.write_text(js)

readme_path = Path('README.md')
readme = readme_path.read_text()
readme = replace_once(readme, 'Åbn **Opgaver → AI Opgaveplanlægger**.', 'Åbn **Indstillinger**, og find **AI Opgaveplanlægger** lige under **Opgavearkiv**.')
readme_path.write_text(readme)
