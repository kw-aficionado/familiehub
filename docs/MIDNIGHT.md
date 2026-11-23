# Automatisk datoskift

Appen skifter den aktuelle dagsvisning ved midnat i **Europe/Copenhagen**.
Det gælder kalenderen, opgavesiden og den separate `/tablet`-visning.
Der er ingen indstilling, der skal aktiveres, og ingen ny tablet-parring.

## Hvad følger datoen?

- En visning af i dag følger med til den nye dato.
- Normalvisningens aktuelle uge følger ugeskift efter familiens valgte ugestart.
  Valget mellem Dag og Uge bevares.
- En bevidst valgt anden dag eller uge bliver stående. Tryk **I dag** for at
  genoptage den aktuelle dagsvisning. Person- og statusfiltre bevares.
- **Familien i dag** og aktuel opfølgning opdateres, selv om kalenderen viser
  en anden dato. Restopgaver behandles efter de allerede valgte regler.
- **Alle opgaver** er stadig et katalog, ikke nye daglige opgavekopier.

En åben formular eller dialog i normalvisningen udsætter selve datoskiftet,
indtil dialogen lukkes. Indtastede titler, startdatoer og planforslag ændres ikke.
Indstillinger lukkes ikke, og eksisterende login og tablet-godkendelser bevares.

## Midnat, dvale og netværk

Det fælles ur kontrollerer datoen på klokkeminutgrænser, ikke 24 timer efter
sidste indlæsning. Sommer-/vintertid og enhedens egen tidszone flytter derfor
ikke familiens datoskift. En synlig, aktiv side henter den nye dag ved midnat.
Browseren kan udsætte timere, mens iPad'en sover; datoen kontrolleres igen ved
synlighed, fokus, `pageshow` og netværksgenoprettelse. Flere dages dvale springer
direkte til den aktuelle dato. Det er ikke en funktion, der vækker iPadOS.

Pauseskærmens egen inaktivitetstid nulstilles ikke af datoskiftet.
Opdateringen genindlæser data, ikke hele siden, og skriver ingen nye opgaver.
Ved manglende netværk vises forbindelsesfejl, og der prøves igen ved kontakt.
En gammel response kan ikke gendanne gårsdagens liste efter et datoskift.
Tablet-afkrydsning er stadig begrænset til serverens aktuelle dag. Et flueben
forbliver bundet til den dato, som det tilhørende opgavekort blev vist for.

## Test

`tests/day_rollover.test.js` tester midnat, sommer/vinter, årsskifte, skuddag,
23-/25-timers døgn, skjult side, genoptagelse, flere dages dvale og udsat skift.
`tests/day_navigation.test.js` tester de faktiske appfunktioner for datoer,
filtre, valg af ugegrænse, kladder og gamle netværkssvar.

`python3 scripts/test-midnight-browser.py --artifacts /tmp/midnight-review`
bygger den rigtige app på loopback med fiktive personer, aftaler, opgaver og
en turnus i en helt ny midlertidig SQLite-database. Kun browseruret simuleres:
fra i går 23.59.58 til serverens faktiske dagsdato. Testen kontrollerer
kalender, fuld opgaveliste, tablet, roterende ansvar, bevaret kladde, valgt
historisk dato, offline/genforbindelse, sidegenoptagelse og smal telefonvisning.
Der kan ikke angives en produktionsadresse eller eksisterende database.
`FH_TEST_CHROMIUM` kan valgfrit angive en lokal Chromium-eksekverbar til testen.

Kør også `sh scripts/check.sh`. Fysisk Safari/iPadOS er en separat enhedstest.
