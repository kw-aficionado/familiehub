# Restopgaver og opfølgning

## Start bevidst

Opdateringen opretter ingen restopgaver eller nye opgaveserier automatisk.
Åbn **Indstillinger → Restopgaver og opfølgning** som administrator.
Gennemgå reglerne for de eksisterende serier, vælg en startdato (standard i dag,
højst 90 dage tilbage), og godkend aktivering. Fra-datoen kan ikke senere flyttes
for at skjule historik. Farvemarkeringens grænse kan fortsat ændres.

## Regler pr. opgave

- **Kun historik for dagen**: fx borddækning til en bestemt aften. Den manglende
  afkrydsning er i voksenoverblikket, ikke en ny pligt i morgen.
- **Rutine: vis manglende afkrydsninger**: fx opvaskemaskine eller skoletaske.
  Dagens kort kan vise antallet af tidligere manglende afkrydsninger i syv dage.
  Der oprettes ikke flere kopier af dagens opgave. Dagens flueben afslutter kun
  dagens forekomst, ikke tidligere dage.
- **Behold som restopgave**: fx rengøring eller en konkret bunke vasketøj.
  Forekomsten bliver i Restopgaver, indtil den afsluttes eller vurderes af en voksen.

Automatisk valg bruger rutine for daglige/hverdagsopgaver og behold for andre.
Navnet bruges aldrig som bevis for, at to opgaver er samme opgave.
Reglen kan sættes i opgavearkivet, ved manuel oprettelse eller på den enkelte
aktive serie i opfølgningspanelet. Planer kopierer skabelonens regel og arbejdsbyrde.
Ændringer gælder kun endnu ikke registrerede forekomster; allerede registrerede
regler, point, ansvar og frister omskrives ikke.

## Frister og ansvar

Dagsopgaver er forsinkede fra næste midnat i Europe/Copenhagen.
Hele-uge-opgaver har frist på sidste dag efter seriens egen gemte ugestart, og
først derefter bliver de restopgaver. Hver-anden-uge-opgaver optræder ikke i
inaktive uger. Sommer-/vintertid ændrer ikke kalenderdagsberegningen.

Restopgaven beholder sin oprindelige ansvarlige ved ugeskift. Ny rotation styrer
kun de nye forekomster. Overdragelse ændrer kun den udvalgte forekomst, og både
oprindelig ansvarlig og faktisk udfører kan fortsat ses.

Orange betyder overskredet frist. Rød betyder normalt mindst to dage efter fristen
eller en anmodning om hjælp. Farven har altid en tekst, og personfarverne bevares.
Udskudte opgaver vises med den aftalte dato, uden at den oprindelige frist fjernes.

## Handlinger og historik

Tilknyttede personlige konti kan afslutte egne restopgaver og bede om hjælp.
En administrator kan desuden aftale ny frist, overdrage, fritage, markere erstattet
eller registrere en faktisk tidligere udførelsesdato. Fritagelse og erstatning
kræver en begrundelse og tæller aldrig som udført. At erstatte en forekomst
markerer ikke automatisk den nye opgave som udført.

Alle handlinger har versionskontrol, gemmekvittering og revisionshistorik.
En forældet visning skal genindlæses. Samtidige handlinger kan ikke overskrive
hinandens afslutning. Gentagelse af samme request-id genbruger kvitteringen.
Gamle afkrydsninger uden faktisk tidsstempel vises som **udført, tidspunkt ukendt**,
ikke som opdigtet rettidig udførelse.

Voksenoverblikket viser afsluttede frister fra de seneste syv dage, opdelt på
oprindelig ansvarlig. Hver forekomst tæller point én gang, ikke for hver forsinket
dag. Fremtidige frister er ikke med, og fritagelser er en separat kategori.
Ældre åbne restopgaver forbliver synlige. Det er ikke en procentvis rangliste.
Historik uden for syvdagesoverblikket bevares i databasen og på forekomstens
historik, men denne version har ikke en fri historikperiodevælger eller CSV-eksport.

Serier med registreret opfølgning kan ikke slettes eller flyttes med MCP.
Brug Quickstart til at stoppe/erstatte fremtidige opgaver; de gamle registreringer
og restopgaver bevares. En aktiveret opfølgning er derfor ikke en oprydning,
der fritager alle gamle opgaver ved at stoppe serien.

## Tablets

Administrator skal særskilt aktivere **Tillad afslutning af synlige restopgaver**
under **Tablets og skærme**. Eksisterende afkrydsningsadgang udvides ikke automatisk.
Kun synlige, forfaldne behold-/hjælpeopgaver kan afsluttes eller bede om hjælp.
Ingen vilkårlig historik, tilbagedatering, fritagelse, overdragelse eller ændring af
regler tillades. Udskudte opgaver kan ikke afsluttes på tabletten før den nye dato.
Tabletten kan stadig se familien samlet; et personfilter er ikke en adgangsgrænse.

## Påmindelser og drift

Eksisterende indstillinger for den samlede daglige påmindelse bevares. Aktivering
af opfølgning aktiverer ingen beskeder, API-nøgler eller AI-udbyder. Fritaget,
erstattet og fremtidigt udskudt arbejde udelades. Dagsspecifikke gamle opgaver
rykkes ikke igen, og rutiner samles efter serie og ansvarlig.
Automatisk eskalering til en voksen og nye ugentlige notifikationer er ikke del
af denne version; hjælpeanmodningen vises i overblikket.

Registrering sker transaktionelt og trinvist ved læsning, afkrydsning og relevante
ændringer. Ved over et års nedetid hentes historikken over flere kald med synlig
advarsel; der slettes ikke gamle forekomster. Gamle manuelt oprettede serier uden
pointdata bruger det tydelige standardestimat 3 point og 15 minutter. Nye planer
og opgaver fra arkivet gemmer de valgte skabelonværdier.

## Test

Kør `sh scripts/check.sh` samt `python3 scripts/test-followup-browser.py`.
Alle data er fiktive og databaser midlertidige. Tests dækker policies, aktivering,
ugestart, rotation, historik, gamle afkrydsninger, arbejdsbyrde, samtidighed,
idempotens, rollback, sessionsrettigheder, CSRF og tablet-tilbagekaldelse.

Ved bagudrettet aktivering bruges de opgaveserier, der findes nu. Historiske
ændringer fra før aktivering kan ikke rekonstrueres. De registrerede forekomster
beskyttes herefter. Dagsskår fra samme AI-plan/skabelon samles som rutinehistorik
efter ansvarlig, aldrig alene fordi titlerne er ens.
