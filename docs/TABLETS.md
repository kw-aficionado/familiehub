# Tablets og skærme

## Forbind en tablet

1. Log ind som administrator på din egen telefon eller computer.
2. Åbn **Indstillinger → Tablets og skærme → Tilføj tablet**.
3. Giv enheden et navn. Vælg eventuelt standardperson, tema og pauseskærm.
   Afkrydsning af dagens opgaver skal aktiveres udtrykkeligt; standard er kun visning.
4. Opret engangskoden. Den gælder i ti minutter og kan bruges én gang.
5. Åbn `/tablet` på FamilieHubs samme HTTPS-adresse på tabletten, og indtast koden.
   Adressen indeholder ingen hemmelighed og kan gemmes som et almindeligt bogmærke.

På iPad: opret gerne hjemmeskærmsgenvejen og åbn den **før** parring, da Safari
og en separat hjemmeskærmsapp kan have forskellig cookielagring. Brug ikke privat
browsing. Hvis cookies slettes, skal administratoren oprette en ny kode.

## Rettigheder og levetid

Tabletten kan se familiens aftaletitler/tider og opgaver. Datoer og personfilter kan
ændres i visningen. Filteret er ikke en privatlivsgrænse. Alle, som fysisk bruger
skærmen, kan læse overblikket og, hvis tilladt, afkrydse/fortryde dagens opgaver.
Den kan ikke redigere eller slette opgaver, aftaler, planer, konti eller forbindelser.
Historiske og fremtidige afkrydsninger kræver normalt login.

Parring afslutter et eventuelt almindeligt login i **samme browser**, ikke andre
personers sessioner. `/tablet` bruger aldrig administratorens cookie som fallback.
Administration fra tabletten er en separat, bevidst loginhandling; log ud bagefter.

En forbindelse udløber efter 30 dage uden kontakt. Ved kontakt fornyes adgangen
højst én gang pr. fem minutter, dog aldrig længere end 180 dage efter parring.
Tabletten viser den seneste dato for ny parring. Udløb kræver en ny engangskode.
Det er en browsergodkendelse, ikke hardware-attestation eller iPadOS-systemlås.

## Fjern adgang og skift enhed

**Fjern adgang** annullerer både aktiv tablet-session og ubrugt parringskode for den
valgte enhed. Serveren afviser efterfølgende requests. Tabletten opdaterer normalt
hvert 30. sekund og rydder visningen ved afvisning. En helt offline enheds allerede
viste data kan ikke fjernslettes. Andre tablets, almindelige konti og familiedata
bevares. **Ny engangskode** afbryder den eksisterende forbindelse straks og kan
bruges til at genforbinde den samme eller en udskiftet tablet.

## Fejl og loginbegrænsning

Almindeligt login: otte fejlslagne forsøg pr. normaliseret kontonavn i et fast
femminuttersvindue. Tomt navn og `admin` er samme konto. Succes nulstiller kontoens
fejl. Der er desuden et separat netværksloft på 120 accepterede loginforsøg pr. IP
og fem minutter, inklusive succeser. Parring er adskilt og har 20 forsøg pr. IP
pr. fem minutter. Fejlformater bliver afvist uden passwordkontrol.

Afviste forsøg forlænger ikke vinduet. Serveren returnerer `Retry-After` og
`retry_after`; begge formularer viser en nedtælling og deaktiverer gentagne klik.
Den server-synlige peer-IP bruges; vilkårlige `X-Forwarded-For`-headers ignoreres.
Bag en proxy kan netværksloftet stadig være fælles. Dette er ikke en påstand om
at have verificeret den aktive Yggdrasil-proxys konfiguration.

Begrænsningsvinduerne er begrænsede i hukommelse og nulstilles ved procesgenstart;
enhedsadgang og spærringer ligger i SQLite. Bevar `/data` og `session.key`.
Normale eksisterende sessioner har stadig deres hidtidige 30-dages levetid.

## Sikkerhedsmodel og tests

Kun administrator kan oprette, redigere og spærre enheder. Parringskode og 256-bit
session-token opbevares som domæneadskilte HMAC-hashes. Kun den nye engangskode
vises ved oprettelse; session-token sendes kun i HttpOnly/SameSite=Strict-cookie
med sti `/api/tablet` og Secure på HTTPS. Koder optræder ikke i URLs eller lokal
JavaScript-lagring. Parring og skrivning kontrollerer JSON, custom-header/CSRF,
Origin (når til stede) og cross-site Fetch Metadata. HTTP er kun tilladt for en
udtrykkeligt konfigureret lokal udviklingsadresse, ikke offentlig drift.

Afkrydsning validerer enhedens aktuelle adgang og opgavens gyldighed i samme
transaktion som skrivningen. En kode kan ikke bruges af to samtidige requests.
Tablet-svaret er en begrænset projektion uden kalendernoter, private feed-adresser,
notifikationstjenester eller plan-/integrationskonfiguration.

Kør `sh scripts/check.sh`. Kør derudover `python3 scripts/test-tablet-browser.py`
med Playwright installeret. Begge bruger kun fiktive data. Unit/integrationstests
dækker scopes, CSRF, samtidighed, udløb, fornyelse, genstart, spærring og limiter.
Browsertesten dækker rigtig parring, dagsliste, afkrydsning, rettighedsændring,
spærring, narrow viewport og nedtælling. Fysisk Safari/iPadOS er en separat kontrol.

Designreferencer: [OWASP Session Management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
og [OWASP Authentication](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html).
