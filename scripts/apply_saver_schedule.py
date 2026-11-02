from pathlib import Path
import hashlib
import re


def checked(path, expected):
    data = Path(path).read_bytes()
    actual = hashlib.sha1(b'blob ' + str(len(data)).encode() + b'\0' + data).hexdigest()
    if actual != expected:
        raise RuntimeError('Source changed; refusing to overwrite ' + path)
    return data.decode('utf-8')


app = checked('web/app.js', '0da0f3149d5476648ca0d80cb676a544ae03e417')
html = checked('web/index.html', '277e4bef204c26a428348ff386678fdcba6daa7b')
begin = app.index('  // Each display has independent light/dark and idle-screen preferences.')
end = app.index('  function accountIsAdmin()', begin)
controller = Path('scripts/saver_controller.txt').read_text()
app = app[:begin] + controller + app[end:]
script = re.findall(r'<script src="/app\.js[^\"]*" defer></script>', html)
assert len(script) == 1
html = html.replace(script[0], '<script src="/saver-schedule.js?v=1" defer></script>\n<script src="/app.js?v=saver-schedule-1" defer></script>')
old_label = '<label class="field">Pausesk\u00e6rm efter<select id="saverDelay">'
assert html.count(old_label) == 1
html = html.replace(old_label, '''<label class="field">Pausesk\u00e6rmens tidsrum<select id="saverMode"><option value="scheduled">Kun i det valgte tidsrum</option><option value="always">Hele d\u00f8gnet</option></select></label>
        <label class="field" id="saverFromField">Aktiv fra<input id="saverFrom" type="time" value="20:00" aria-describedby="saverScheduleStatus"></label>
        <label class="field" id="saverToField">Aktiv indtil<input id="saverTo" type="time" value="07:00" aria-describedby="saverScheduleStatus"></label>
        <label class="field">Pausesk\u00e6rm efter inaktivitet<select id="saverDelay">''')
button = '<button type="button" class="minor" id="saverPreviewBtn">Afpr\u00f8v pausesk\u00e6rmen</button>'
assert html.count(button) == 1
html = html.replace(button, '<p id="saverScheduleStatus" class="muted" role="status" aria-live="polite"></p>\n       ' + button + '\n       <p class="muted">Tidsrummet f\u00f8lger dansk tid og er uafh\u00e6ngigt af m\u00f8rkt tema. V\u00e6lg Aldrig for at sl\u00e5 automatisk pausesk\u00e6rm fra. Afpr\u00f8v virker ogs\u00e5 uden for tidsrummet.</p>')
Path('web/app.js').write_text(app, encoding='utf-8')
Path('web/index.html').write_text(html, encoding='utf-8')
