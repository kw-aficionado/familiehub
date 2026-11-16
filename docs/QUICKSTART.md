# Quickstart, turnus og mulige dubletter

## En opgave, flere deltagere

Åbn **Indstillinger → Quickstart og turnus → Quickstart – del opgaver**.
Vælg startdato, ugestart og kun de opgaver, der skal oprettes.
For hver opgave vælges deltagere, startperson, gentagelse og interval for bytte.

- **Én fælles opgave på skift**: én serie og én ansvarlig ad gangen.
- **Personlig opgave til hver deltager**: en selvstændig serie og afkrydsning
  til hver person. Brug eksempelvis dette til lektier og skoletaske.
- Gentagelser: dagligt, hverdage, en bestemt ugedag, i løbet af en uge,
  hver anden uge eller en enkelt dag/uge. Gentagelse er ikke det samme som
  bytteinterval: en daglig opgave kan skifte ansvarlig hver uge.

Startpersonen har startugens ansvar. En kort startuge kan have færre eller ingen
forekomster for en opgave med bestemt ugedag. Kontroller de fire viste uger:
bytte følger ugegrænsen, ikke syv dage efter første udførelse.
Point og minutter er faktiske forekomster i hver preview-uge; hver-anden-uge-opgaver
indgår ikke i deres inaktive uger. Quickstart er en manuel fordeling, mens den
separate AI Opgaveplanlægger fortsat tilbyder automatisk fordeling.

## Ugestart uden historikomskrivning

Familiens ugestart er fælles og kan ændres af administratoren. Standard er mandag.
Det styrer visningen af uger og standarden for nye planer. Hver plan gemmer sit
eget valg. Eksisterende serier migreres til mandag, deres hidtidige grænse.
Ved en anden ugestart kan ugevisningen indeholde to forskellige vinduer fra en
ældre ugeopgave; de har forskellige afkrydsninger og vises derfor hver for sig.
Påmindelser følger opgavens egen sidste ugedag og den rigtige ansvarlige pr. uge.

## Gennemgå og stop gamle serier

Start med **Gennemgå opgaveserier / mulige dubletter**. Oversigten viser alle
serier, også dem der er oprettet manuelt eller kommer fra ældre planer.
Flere opgaver med samme titel er kun et muligt overlap, aldrig grund til
at slette eller sammenflette dem automatisk.

Administrator kan markere de konkrete gamle serier og vælge en stopdato fra
**i dag eller senere**. En bekræftelse viser de valgte titler og serie-id'er.
Rækker og afkrydsninger slettes ikke. Stoppede serier forbliver i denne oversigt,
men vises ikke i den aktive katalogliste fra stopdatoen og sender ikke nye
påmindelser. Historiske forekomster før stopdatoen er bevaret.

Er der afkrydsninger i eller efter stopperioden, afvises stoppet. For en allerede
udført ugeopgave skal stopdatoen derfor ligge efter dens fulde uge. Det beskytter
historikken i stedet for at nulstille eller flytte afkrydsninger.

Quickstarts preview viser overlappende eksisterende serier. De kan markeres til
stop fra den nye plans startdato. Stop og nye opgaver gemmes i **samme transaktion**.
Skal de øvrige serier bevares, kræver det en eksplicit accept af overlap.
Dette er ikke en automatisk reparation af den aktive families database.

## Godkendelse og genforsøg

Preview ændrer ikke opgaver. Et hash sikrer, at gemning svarer til det viste
forslag og konfliktgrundlag; ændrede opgaver eller arkivværdier kræver et nyt
forslag. Gemning serialiserer læsning, kontrol og alle skrivninger.

Et gentaget request-id eller en identisk aktiv plan genbruger gemmekvitteringen.
Samme request-id med andet indhold afvises. En plan, der siden er flyttet,
stoppet eller slettet, genoprettes ikke skjult af et genforsøg.
Gamle dubletter fra før denne opdatering skal stadig gennemgås eksplicit.

## Test

`quickstart_test.go` dækker ugegrænser, personlige/fælles opgaver, rotation,
belastning, idempotens, samtidige gemmeforsøg, rollback, historik, API/CSRF og
påmindelser. Node-tests dækker navigation, kildetekster og indstillinger.
`scripts/test-planning-browser.py` udfører det rigtige brugerforløb med fiktive data.
