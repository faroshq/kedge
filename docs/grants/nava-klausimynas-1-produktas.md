# NAVA – klausimyno 1 dalies atsakymai (produktas ir sprendimas)

> **Pastaba dėl pavadinimo:** „NAVA" – darbinis (kodinis) pavadinimas; galutinis
> produkto prekės ženklas bus patvirtintas atlikus prekės ženklo ir SEO analizę,
> todėl viešai naudojamas pavadinimas gali skirtis.
>
> Suderinta su: [`nava-projekto-aprasymas.md`](./nava-projekto-aprasymas.md),
> [`nava-project-description-en.md`](./nava-project-description-en.md),
> [`nava-klausimyno-atsakymai.md`](./nava-klausimyno-atsakymai.md).

---

## 1.1. Detaliai aprašytas planuojamas kurti produktas ar sprendimas, nurodant jo pagrindinius funkcionalumus, veikimo principus ir, jei taikoma, numatomas integracijas.

„NAVA" – **daugiaklientė (angl. *multi-tenant*) platforma, kurioje organizacija
valdo savo DI agentus, aplikacijas ir infrastruktūrą vienoje valdymo
plokštumoje**. Vartotojas natūralia kalba paveda užduotį DI agentui; agentas
planuoja veiksmus, naudoja įrankius ir atlieka operacijas realioje kliento
aplinkoje (Kubernetes klasteriuose, serveriuose, debesijos paskyrose)
laikydamasis organizacijos nustatytų saugumo ribų.

Platformos architektūrinis pagrindas – **įskiepiamų tiekėjų (angl. *provider*)
modelis** ant atvirojo kodo daugiaklientės valdymo plokštumos „kcp". Kiekvienas
tiekėjas yra savarankiškas modulis, atnešantis: savo API, savo valdiklius, savo
mikro-priekinę dalį (angl. *micro-frontend*) portale ir savo įrankių šeimą DI
agentams. Klientas portale pasirenka, kuriuos tiekėjus įjungti savo darbo
erdvėje (angl. *workspace*); branduolys veikia ir be nė vieno pasirenkamo
tiekėjo.

**Pagrindiniai funkcionalumai:**

1. **Autonominiai DI agentai.** Kiekvienas agentas turi personą (sisteminį
   kontekstą), ilgalaikę atmintį, priskirtus modelių profilius ir įrankių
   rinkinį. Veikimo principas – LLM „įrankių kilpa" (angl. *tool loop*):
   planavimas → įrankio iškvietimas → rezultato stebėjimas → tęsinys, kartojama
   iki užduoties įvykdymo arba iki nustatytos ribos (numatytoji riba – 16
   įrankių ciklų vienam paleidimui, konfigūruojama). Agentas gali deleguoti dalį
   užduoties kitam agentui (angl. *sub-agent*) su bendra biudžeto apskaita.

2. **Nuolatinis, nuo pokalbio nepriklausomas vykdymas.** Agentai veikia ne tik
   atsakydami į žinutes: pagal **tvarkaraščius** (laiko juostą palaikantis
   „cron"), pagal **vienkartinius pažadinimus** (angl. *wakeup*; agentas pats
   užsirašo „patikrinti po 2 val."), pagal **periodinius „širdies plakimus"**
   (angl. *heartbeat*; agentas peržiūri savo standartinį patikrų sąrašą ir tyli,
   jei nėra ko eskaluoti) ir pagal **įvykių trigerius** (angl. *event trigger*;
   išoriniai „webhook", „GitHub" ar pokalbių kanalo įvykiai).

3. **Saugi prieiga prie kliento infrastruktūros.** Kiekvienoje kliento aplinkoje
   veikiantis agentinis komponentas inicijuoja **išeinantį atvirkštinį tunelį**
   (angl. *outbound reverse tunnel*), todėl sistemos už NAT ir ugniasienių tampa
   pasiekiamos per vieną autentifikuotą tašką – **be VPN, be atidarytų prievadų,
   be „kubeconfig" failų platinimo**. Palaikomos dvi aplinkų rūšys: Kubernetes
   klasteriai ir atskiri serveriai (SSH).

4. **Aplikacijų šablonai (angl. *Template*) – „auksinis kelias" (angl. *golden
   path*).** Platformos arba organizacijos inžinieriai vieną kartą aprašo, kaip
   turi atrodyti aplikacijos infrastruktūra (pvz., „priekinė dalis + serverinė
   dalis + PostgreSQL + OIDC apsaugotas URL"), o vartotojai – įskaitant
   netechninius – kuria aplikacijas iš šių šablonų per portalą arba tiesiog
   paprašę DI agento. Rezultatas visada atitinka įmonės saugumo ir eksploatacijos
   standartus, nes pasirinkimo laisvė apribota šablono schema, o ne vartotojo
   drausme.

5. **Žmogaus kontrolė pagal dizainą.** Agento galios priklauso nuo to, **kas jį
   paleido ir ar žmogus stebi** – tai konteksto lygmens patikimumo modelis
   (angl. *trigger-scoped trust*): interaktyviame pokalbyje rizikingi įrankiai
   leidžiami, bet gali reikalauti patvirtinimo; neprižiūrimi (suplanuoti,
   „širdies plakimo", trigerio) paleidimai pagal nutylėjimą turi tik skaitymo
   teises. **Patvirtinimų dėžutė** (angl. *approvals inbox*) veikia ir portale,
   ir pokalbių kanale. Kiekvienas įrankio iškvietimas fiksuojamas audito
   žurnale.

6. **Skaitmeninis dvynys (angl. *digital twin*) – viso ūkio (angl. *fleet*)
   lygmens užklausų sluoksnis.** Objektai iš visų prijungtų kliento aplinkų
   nepertraukiamai sinchronizuojami į vieną indeksą (Kubernetes stebėjimo srautai,
   angl. *informer*, per tuos pačius atvirkštinius tunelius). Ant indekso veikia
   ryšių traversavimas: nuosavybės grandinės, specifikacijos laukų nuorodos,
   žymių selektoriai, tarpklasteriniai ryšiai. Vienas užklausimas atsako į
   klausimą „kurios iš 50 aplinkų naudoja X atvaizdą" arba „kas naudoja šią
   paslaptį – ar saugu ją pakeisti", kai alternatyva būtų dešimtys „kubectl"
   kreipimųsi per lėtus ryšius. Tas pats sluoksnis prieinamas DI agentui kaip
   įrankis – tai tiesiogiai mažina agento sąnaudas ir klaidas.

7. **Plečiama tiekėjų architektūra.** Tiekėjas platinamas kaip vienas „Helm"
   paketas; platforma jį automatiškai aptinka, sukuria jam izoliuotą valdymo
   plokštumos darbo erdvę, sugeneruoja prieigos kredencialus ir integruoja jo
   sąsają į portalą. Trečioji šalis – vendorius, sistemų integratorius ar pati
   kliento organizacija – gali sukurti savą tiekėją ir taip išplėsti platformos
   bei agentų galimybes be pagrindinės sistemos perkūrimo. Tuo pačiu mechanizmu
   kuriami ir visi pačios platformos moduliai: agentai, aplikacijų kūrimo
   aplinka, infrastruktūros šablonai, kodo saugyklos, aplinkų prijungimas,
   užklausų sluoksnis – tiekėjų architektūra yra platformos pamatas, o ne
   priedas.

8. **Daugiakanalis bendravimas.** Su agentu bendraujama iš portalo, iš įprastų
   pokalbių kanalų („Telegram", „Slack", „Discord"), el. paštu (išeinantys
   pranešimai), taip pat per **Model Context Protocol (MCP)** – t. y. iš
   išorinių DI klientų. Ryšys dvipusis: kanale galima ne tik gauti pranešimą,
   bet ir patvirtinti ar atmesti agento prašomą veiksmą.

9. **Naudojimo apskaita, kvotos ir biudžetai.** Kiekvienam agentui nustatoma
   mėnesinė LLM sąnaudų riba, tikrinama prieš kiekvieną paleidimą. Platformos
   lygmeniu veikia atskiras apskaitos sluoksnis, renkantis naudojimo rodiklius
   iš visų paskyrų – jis maitina ir kvotų kontrolę, ir prenumeratos
   apmokestinimą.

**Integracijos:** LLM tiekėjai per „OpenAI"-suderinamą sąsają (įskaitant
savarankiškai talpinamus modelius), „GitHub" (kodo saugyklos, „OAuth",
nepertraukiamo integravimo konvejeriai), Model Context Protocol (bet kokia
išorinė sistema, turinti MCP serverį), „Telegram" / „Slack" / „Discord" / SMTP,
Kubernetes ir SSH aplinkos per atvirkštinius tunelius, OIDC tapatybės tiekėjai
(„Dex", „Auth0", „Okta", „Entra"), „Stripe" (prenumeratų apmokestinimas).

**Diegimo modeliai:** valdoma SaaS paslauga arba savarankiškai diegiamas (angl.
*self-hosted*) paketas („Helm") kliento infrastruktūroje. Komercinis modelis –
atviro branduolio (angl. *open core*).

Visi išvardyti funkcionalumai yra projekto apimties darbai; jų kūrimo seka ir
etapų rezultatai pateikti 1.17 punkte.

---

## 1.2. Koks yra pagrindinis DI, blokų grandinės technologijų, robotikos procesų automatizavimo produkto ir (ar) sprendimo tikslas (pvz.: automatizacija, prognozavimas, analizė, klientų aptarnavimas ir pan.)?

Pagrindinis tikslas – **autonominis IT operacijų automatizavimas ir netechninių
vartotojų įgalinimas savarankiškai kurti bei paleisti aplikacijas nereikalaujant
infrastruktūros žinių**.

Tai du to paties tikslo galai:

- **Automatizavimas.** Pasikartojančios, rankinės DevOps/SRE užduotys –
  diegimai, incidentų tyrimas, konfigūracijų atnaujinimai, atitikties patikros,
  ataskaitos – perduodamos savarankiškai sprendimus priimantiems DI agentams,
  veikiantiems interaktyviai, pagal tvarkaraštį arba pagal įvykius.
- **Įgalinimas.** Organizacijos inžinieriai vieną kartą aprašo leistinas
  aplikacijų formas (šablonus), o produkto ar verslo komandos jomis naudojasi
  savarankiškai – per portalą arba paprašiusios DI agento. Taip pašalinamas
  „bilietas DevOps komandai" kaip privalomas žingsnis tarp idėjos ir veikiančios
  aplikacijos.

Šalutiniai, bet matuojami tikslai: greitesnis incidentų sprendimas, mažesnė
žmogiškųjų klaidų rizika ir saugi prieiga prie paskirstytos infrastruktūros be
VPN.

Tikslinė auditorija – DevOps ir platformų komandos, valdomų paslaugų teikėjai
(angl. *managed service provider*, MSP) bei vidutinės IT organizacijos, kuriose
šias operacijas šiandien atlieka trūkstami ir brangūs specialistai.

---

## 1.3. Kokią konkrečią problemą sprendžia šis DI, blokų grandinės technologijų, robotikos procesų automatizavimo produktas ir (ar) sprendimas? Kas šiuo metu atlieka šią funkciją (žmonės, esami įrankiai) ir kokie yra dabartinės situacijos trūkumai?

**Problema.** Šiuolaikinės organizacijos valdo vis labiau paskirstytą ir
fragmentuotą infrastruktūrą: keli Kubernetes klasteriai, kelios debesijos
paskyros, pakraščio (angl. *edge*) įrenginiai, vietiniai serveriai. Dvi
skirtingos šio fragmentiškumo pasekmės:

1. **Operacijos atliekamos rankomis.** Kasdienius veiksmus šiandien atlieka
   DevOps/SRE specialistai – jų trūksta, jie brangūs, o darbas pasikartojantis
   ir klaidoms imlus.
2. **Atsiradus DI agentams, kliūtis persikėlė, bet neišnyko.** Netechninis
   vartotojas jau geba su DI pagalba sukurti aplikacijos kodą, tačiau už jos
   **paleidimą, saugumą ir palaikymą** vis tiek atsako DevOps/SRE komanda. Tarp
   „kodas parašytas" ir „aplikacija veikia produkcijoje pagal įmonės standartus"
   lieka rankinis, žmogiškas tarpininkavimas.

„NAVA" sprendžia abu: agentai perima operacijas, o šablonai leidžia inžinieriams
**vieną kartą apibrėžti leistiną aplikacijos formą** ir toliau leisti vartotojams
kurti aplikacijas savarankiškai – savaime atitinkančias įmonės saugumo
standartus.

**Kas atlieka šią funkciją šiandien ir kokie trūkumai:**

| Dabartinis būdas | Trūkumai |
|---|---|
| DevOps/SRE specialistai rankiniu būdu | Brangu, lėta, priklauso nuo trūkstamų kompetencijų, neskaluojasi |
| Scenarijais paremta automatizacija (skriptai, CI/CD, „runbook"-ai) | Trapi – reikalauja iš anksto numatyti kiekvieną atvejį; nuolatinė priežiūra; neapima nenumatytų situacijų |
| Tradicinis robotizuotas procesų automatizavimas (angl. *RPA*) | Orientuotas į vartotojo sąsajų imitavimą, ne į infrastruktūros ir API lygmens operacijas |
| DI pokalbių asistentai ir „vibe coding" įrankiai | Neturi saugios, audituojamos veikimo prieigos prie realios infrastruktūros; nedirba autonomiškai pagal tvarkaraštį ar įvykius; sukuria kodą, bet ne veikiančią, standartus atitinkančią aplikaciją |
| Vidinės kūrėjų platformos (angl. *internal developer platform*, IDP) | Sprendžia „auksinio kelio" dalį, bet yra pasyvūs katalogai – nieko neatlieka patys, neturi autonominių agentų ir neapima paskirstytos infrastruktūros prieigos |

---

## 1.4. Kokia finansinė ar nefinansinė vertė (pvz.: efektyvumo augimas procentais, kaštų mažinimas, naujos pajamos, reputacija), tikimasi, bus sukurta įmonei? Kas bus galutinis naudotojas (fiziniai ir (ar) juridiniai asmenys) ir koks bus poveikis naudotojui?

**Galutiniai naudotojai – juridiniai asmenys** (B2C nenumatoma): DevOps ir
platformų komandos, valdomų paslaugų teikėjai (MSP), vidutinės IT organizacijos
ir technologijų įmonės su paskirstyta infrastruktūra Lietuvoje ir ES. Antrinė
naudotojų grupė toje pačioje organizacijoje – produkto ir verslo komandos, kurios
naudojasi platforma per šablonus ir agentus neturėdamos infrastruktūros
kompetencijos.

**Vertė naudotojui:**

- operacinių sąnaudų mažinimas – konservatyviai vertinant, viena „NAVA" agentų
  aplinka gali perimti pasikartojančias operacijas, prilygstančias apytiksliai
  vienam etatui; skaičiuojant vidutines ~3 000 EUR/mėn. kvalifikuoto DevOps/SRE
  specialisto darbo sąnaudas, tai sudaro **~36 000 EUR per metus** sutaupytų
  operacinių sąnaudų vienam klientui;
- trumpesnis kelias nuo idėjos iki veikiančios aplikacijos, nes netechninė
  komanda nebepriklauso nuo DevOps komandos eilės;
- mažesnė žmogiškųjų klaidų ir konfigūracijos nuokrypio (angl. *configuration
  drift*) rizika – aplikacijos materializuojamos iš patvirtintų šablonų, o ne
  kuriamos rankomis;
- greitesnis incidentų sprendimas;
- saugi prieiga prie paskirstytos infrastruktūros be VPN ir be atidarytų
  prievadų.

**Vertė įmonei (pareiškėjui):** pasikartojančios SaaS prenumeratos pajamos,
eksportuojamas produktas, atvirojo kodo bendruomenės kuriama reputacija ir
tiekėjų ekosistema, didinanti produkto vertę be proporcingų mūsų pačių
investicijų.

**Svarbu:** aukščiau pateikti dydžiai yra **tikslinės, dar nepasiektos
reikšmės**. Matavimo būdas: pilotinių diegimų metu bus registruojamas agentų
perimtų operacijų skaičius, jų vidutinė trukmė atliekant rankomis ir su tuo
susijęs sutaupytas kvalifikuoto darbuotojo laikas; „auksinio kelio" naudai – nuo
užsakymo iki veikiančios aplikacijos praeinantis laikas prieš ir po įdiegimo.

---

## 1.5. Argumentuokite, kodėl šiai problemai išspręsti nepakanka egzistuojančių technologijų ar rinkoje esančių sprendimų. Kuo kuriamas DI, blokų grandinės technologijų, robotikos procesų automatizavimo produktas ir (ar) sprendimas techniškai ir funkciškai skiriasi nuo esamų alternatyvų?

Peržiūrėtuose viešuose šaltiniuose artimiausia atviro kodo alternatyva –
**Aurora (Arvo AI)**: taip pat savarankiškai diegiamas, modelių atžvilgiu
neutralus DI agentas infrastruktūros operacijoms. Tačiau ji orientuota į
**incidentų tyrimą** (reaguoja į jau įvykusią problemą), neturi daugiaklientės
SaaS versijos ir neapima infrastruktūros valdymo bei aplikacijų paleidimo.

Uždaros SaaS platformos (**„Cleric"**, **„Resolve AI"**) sprendžia panašią
problemą, bet reikalauja siųsti duomenis į išorinę, ne ES jurisdikcijoje
veikiančią platformą, neturi savarankiškai diegiamos versijos, o jų veikimo
logikos negalima audituoti kodo lygmeniu. „Cleric" pagal viešą gamintojo
dokumentaciją kliento aplinkose veikia tik skaitymo režimu – veiksmų apskritai
neatlieka. Atvirojo kodo Kubernetes diagnostikos agentai (**„HolmesGPT"**,
**„K8sGPT"**) taip pat yra tik skaitymo režimo tyrimo įrankiai be daugiakliento
ir be autonominio vykdymo pagal tvarkaraščius (išsamiau – 1.6).

Arčiausiai suvereniteto krypties – **„Hyground"** (Vokietija): uždaras, bet
savarankiškai diegiamas DI SRE agentas, veikiantis kliento Kubernetes
klasteryje (referencinis klientas – „Deutsche Bahn"). Jis patvirtina, kad
suverenaus, savarankiškai diegiamo DI operacijų sprendimo paklausa Europoje
reali, tačiau nėra atviro kodo, neturi daugiaklientės platformos ir aplikacijų
šablonų bei neaprėpia ne Kubernetes serverių.

Vidinių kūrėjų platformų (**Backstage**, **Port**, **Humanitec**) klasė sprendžia
„auksinio kelio" dalį – šablonus ir savitarną – bet tai pasyvūs katalogai:
jie nieko neatlieka patys, neturi autonominių agentų ir neturi prieigos prie už
ugniasienės esančios infrastruktūros modelio.

**Techninis ir funkcinis skirtumas – sujungimas, kurio atskirai neturi nė vienas
iš jų:**

1. **Nuolatinis, bendros paskirties autonomiškumas** – tvarkaraščiai, „širdies
   plakimai" ir įvykių trigeriai bet kuriai operacijai, o ne tik incidentų
   tyrimui, su konteksto lygmens patikimumo politika kaip pagrindine apsauga nuo
   netiesioginės komandų injekcijos (angl. *indirect prompt injection*).
2. **Dvigubas tiekimo modelis** – ta pati platforma veikia ir kaip daugiaklientė
   SaaS, ir kaip savarankiškai diegiamas paketas kliento infrastruktūroje.
3. **Atvirkštinių tunelių prieigos architektūra** – agentai saugiai veikia už
   ugniasienės be VPN, be atidarytų prievadų, su pilnu auditu.
4. **Plečiama trečiųjų šalių tiekėjų architektūra** ant atvirojo kodo valdymo
   plokštumos – platformos galimybės auga už mūsų įtakos ribų.
5. **Agentas ir „auksinis kelias" toje pačioje sistemoje** – agentas kuria
   aplikacijas ne laisva forma, o iš organizacijos patvirtintų šablonų, todėl
   autonomija neprieštarauja atitikčiai.

---

## 1.6. Aprašykite esamus rinkoje panašius DI, blokų grandinės technologijų, robotikos procesų automatizavimo produktus ir (ar) sprendimus (bent 3–5 konkurentus ar alternatyvas), pateikite palyginimus (jei įmanoma, palyginkite konkrečius parametrus) ir paaiškinkite, kokiu konkurenciniu pranašumu ar naujumu vartotojams pasižymės jūsų DI, blokų grandinės technologijų, robotikos procesų automatizavimo produktas ir (ar) sprendimas.

**Rinkos kontekstas.** DI agentų rinka pasaulyje vertinama ~7,8 mlrd. USD
(2025 m.) ir prognozuojama augsianti iki ~52,6 mlrd. USD 2030 m. (~46 %
vidutinis metinis augimas; „MarketsandMarkets"); „Gartner" prognozuoja, kad iki
2026 m. pabaigos 40 % įmonių programų turės įterptus užduočių DI agentus
(2025 m. – mažiau nei 5 %). Suvereniteto segmentas auga dar sparčiau: Europos
suvereniteto debesijos išlaidos 2026 m. prognozuojamos ~12,6 mlrd. USD
(+83 % per metus; „Gartner"), o ES „Apply AI" strategija ir „Cloud and AI
Development Act" viešajam sektoriui skatina „pirk europietišką" atvirojo kodo
DI principą. Konkurentų pritraukiamo kapitalo apimtys (žr. žemiau) šį augimą
patvirtina.

Konkurentų analizė atlikta 2026 m. liepos mėn. pagal viešai prieinamus
šaltinius: produktų dokumentaciją, viešas kodo saugyklas, skelbiamą kainodarą
ir finansavimo pranešimus. Apžvelgtos trys alternatyvų klasės: (a) DI SRE ir
operacijų agentai – artimiausia klasė, (b) atvirojo kodo Kubernetes DI
įrankiai, (c) horizontalios automatizavimo ir vidinių kūrėjų platformos.

**(a) DI SRE ir operacijų agentai:**

- **„Resolve AI" (JAV)** – uždaras SaaS DI agentas gamybinių sistemų priežiūrai
  ir incidentų tyrimui, 2024 m. įkurtas buvusių „Splunk" / „OpenTelemetry"
  vadovų. Rinkos brandos rodiklis: pritraukė daugiau nei 190 mln. USD ir
  2026 m. balandį vertintas ~1,5 mlrd. USD; klientai – „Coinbase", „DoorDash",
  „MongoDB". Viešos kainodaros nėra (tik individualūs pardavimai), produktas
  orientuotas į labai stambias įmones. Kodas uždaras, savarankiškai diegiamos
  versijos nėra; nesprendžia aplikacijų paleidimo, „auksinio kelio" ir prieigos
  už ugniasienės.
- **„Cleric" (JAV)** – uždaras SaaS „DI SRE" agentas, tiriantis gamybinius
  aliarmus ir pateikiantis diagnozes „Slack" kanale. Pagal viešą gamintojo
  dokumentaciją kliento aplinkose veikia **tik skaitymo režimu** – veiksmų
  neatlieka, tik siūlo. Savarankiško diegimo nėra, kainodara individuali.
- **„Kubiya.ai" (Izraelis / JAV)** – uždara agentinės DevOps automatizacijos
  platforma su hibridiniu vykdymu (dalis vykdytojų gali veikti kliento
  aplinkoje). Vieša kainodara – nuo ~15 000 USD/metus (startuolių planas) iki
  72 000 ir daugiau USD/metus, su modelio žetonų kvotomis pagal planą.
  Orientuota į iš anksto aprašytų darbo eigų vykdymą; be atviro kodo, be
  šablonų sluoksnio, be atvirkštinių tunelių prieigos modelio.
- **„Aurora" („Arvo AI", Kanada)** – artimiausia **atvirojo kodo** alternatyva
  („Apache 2.0" licencija): „LangGraph" agentai, tiriantys incidentus AWS /
  „Azure" / GCP / Kubernetes aplinkose; diegiama per „Helm", savarankiškai
  diegiant nemokama. Orientuota į **incidentų tyrimą** – reaguoja į jau
  įvykusią problemą; neturi daugiaklientės SaaS, bendros paskirties autonomijos
  (tvarkaraščių ir trigerių bet kuriai operacijai), aplikacijų šablonų ir
  prieigos už ugniasienės modelio. Bendruomenė kol kas maža (apie 400 „GitHub"
  žvaigždučių, ~11 aktyvių kūrėjų).
- **„Hyground" (Vokietija)** – uždaras, bet **savarankiškai diegiamas
  „suverenus DI SRE agentas Europai"**: veikia kliento Kubernetes klasteryje,
  palaiko atjungtą (angl. *air-gapped*) diegimą, klientas pats parenka modelius,
  vykdo ir suplanuotas operacijas. €3 mln. pradinis finansavimas (2026 03,
  „Partech"); referencinis klientas – „Deutsche Bahn". Skirtumai nuo „NAVA":
  ne atviras kodas, be daugiaklientės SaaS (vienos organizacijos licencija),
  tik Kubernetes (be serverių ir be tunelių už perimetro), be aplikacijų
  šablonų.
- **Stambieji tiekėjai** – **„Microsoft Azure SRE Agent"** (bendrai prieinamas
  nuo 2026 03; veiksmai su patvirtinimais arba „privilegijuotu" autonominiu
  režimu; tik „Azure" SaaS) ir **„Datadog Bits AI"** (SRE agentas nuo 2025 12,
  autonominio taisymo peržiūros versija nuo 2026 06; už ugniasienės veikiantis
  „Private Action Runner" vykdiklis; ~6,5 USD už tyrimą). Abu uždari, be
  savarankiško diegimo ir be šablonų – bet patvirtina, kad rinka juda nuo
  tyrimo prie **vykdančių** agentų.

**(b) Atvirojo kodo Kubernetes DI įrankiai.** „HolmesGPT" (vysto „Robusta" ir
„Microsoft"; CNCF „Sandbox" projektas) ir „K8sGPT" – incidentų tyrimo agentai,
pagal dizainą veikiantys tik skaitymo režimu, be daugiakliento ir be
autonominio vykdymo pagal tvarkaraščius. „kagent" (CNCF „Sandbox", remia
„Solo.io") – brandžiausias atvirojo kodo agentų vykdymo karkasas Kubernetes
aplinkai (patvirtinimų vartai, įvykių trigeriai, pokalbių kanalai), tačiau
daugiaklientiškumas jame viešai įvardytas kaip neišspręsta problema
(rezervuota komerciniam produktui), agentai veikia klasterio viduje –
priešinga architektūra nei centras su tuneliais – ir nėra nei aplikacijų
šablonų, nei ne Kubernetes serverių palaikymo. Šie projektai patvirtina rinkos
kryptį, bet nė vienas nesiūlo daugiaklientės platformos su „auksiniu keliu";
nemokamo savarankiškai diegiamo daugiaklientiškumo nesiūlo nė vienas
apžvelgtas atvirojo kodo projektas – jis visur apmokestintas arba draudžiamas
licencijos sąlygų.

**(c) Horizontalios platformos.** „n8n" (angl. *fair-code* licencija;
savarankiškai diegiama darbo eigų ir DI agentų platforma) reikalauja kiekvieną
darbo eigą sukonstruoti rankiniu būdu ir neturi nei infrastruktūros prieigos
modelio, nei šablonų. „Backstage" / „Port" / „Humanitec" (vidinės kūrėjų
platformos, IDP) sprendžia šablonų ir savitarnos dalį, bet istoriškai yra
pasyvūs katalogai be autonominių agentų. Dvi konvergencijos kryptys verčia
šią klasę judėti: „Port" 2025 12 pritraukė 100 mln. USD „agentinei inžinerijos
platformai" (agentai vykdo savitarnos veiksmus su patvirtinimais, bet skirta
tik inžinieriams), o „Qovery" („AI Builder", 2026 05) sujungė šablonus su
įterptais kodavimo agentais netechniniams vartotojams – tačiau uždaroje, vieno
tiekėjo, darbo aplinkos (o ne viso aplikacijos gyvavimo ciklo) apimtyje. Abi
patvirtina agentų ir „auksinio kelio" konvergenciją; nė viena nejungia atviro
kodo, daugiakliento modelio ir aplikacijų gyvavimo ciklo.

**Palyginimas pagal parametrus:**

| Sprendimas | Tipas | Atviras kodas | Savarankiškas diegimas | Daugiaklientė SaaS | Autonomija (tvarkaraščiai / įvykiai) | Vykdo veiksmus | Prieiga už ugniasienės | Aplikacijų „auksinis kelias" | Kainodaros modelis |
|---|---|---|---|---|---|---|---|---|---|
| **„NAVA"** | DI agentų ir operacijų platforma | ✅ | ✅ | ✅ | ✅ bendros paskirties | ✅ su patvirtinimais | ✅ atvirkštiniai tuneliai | ✅ šablonai | atviras branduolys, SaaS kvotų principu |
| **„Resolve AI"** | DI SRE agentas | ❌ | ❌ | ✅ | ribota (incidentai / operacijos) | ✅ | ❌ | ❌ | individuali, stambioms įmonėms |
| **„Cleric"** | DI SRE agentas | ❌ | ❌ | ✅ | ribota (aliarmų tyrimas) | ❌ tik skaitymas | ❌ | ❌ | individuali |
| **„Kubiya.ai"** | agentinė DevOps platforma | ❌ | dalinis (hibridinis) | ✅ | ribota (darbo eigos) | ✅ | dalinė (vietiniai vykdytojai) | ribotas | ~15 000–72 000+ USD/metus |
| **„Hyground"** | suverenus DI SRE agentas | ❌ | ✅ (vienintelis režimas) | ❌ | ribota (incidentai + tvarkaraščiai) | ✅ | veikia perimetro viduje (tik K8s) | ❌ | metinė licencija pagal infrastruktūrą |
| **„Azure SRE Agent" / „Datadog Bits AI"** | stambiųjų tiekėjų DI SRE agentai | ❌ | ❌ | ✅ | ribota (incidentai / operacijos) | ✅ su patvirtinimais | dalinė (išeinantys vykdikliai) | ❌ | kreditai pagal naudojimą |
| **„Aurora" („Arvo AI")** | DI SRE agentas | ✅ („Apache 2.0") | ✅ | ❌ | ribota (incidentai) | ✅ | ❌ | ❌ | nemokama (atviras kodas) |
| **„HolmesGPT" / „K8sGPT"** | Kubernetes tyrimo agentai | ✅ | ✅ | ❌ | ❌ | ❌ tik skaitymas | ❌ | ❌ | nemokama (atviras kodas) |
| **„kagent"** | agentų karkasas Kubernetes aplinkai | ✅ | ✅ | ❌ | dalinė | priklauso nuo sukonstruoto agento | ❌ | ❌ | nemokama (atviras kodas) |
| **„n8n"** | darbo eigų ir DI agentų platforma | angl. *fair-code* | ✅ | ✅ | ✅ (rankiniu būdu aprašytos eigos) | ✅ | ❌ | ❌ | nemokama + prenumerata |
| **„Backstage" / „Port" / „Humanitec"** | vidinė kūrėjų platforma (IDP) | dalinis („Backstage" ✅) | ✅ | ribota | ❌ | ❌ pasyvus katalogas | ❌ | ✅ | įvairi |

**Konkurencinis pranašumas – derinys, kurio neturi nė viena alternatyva:**

1. **Vienintelis sprendimas, kuris kartu yra atvirojo kodo, savarankiškai
   diegiamas ir daugiaklientė SaaS.** Uždari konkurentai neturi savarankiško
   diegimo, atvirieji – daugiaklientės SaaS; „NAVA" aprėpia abu tiekimo
   modelius viena kodo baze.
2. **Bendros paskirties autonomija** – tvarkaraščiai, „širdies plakimai" ir
   įvykių trigeriai bet kuriai operacijai, o ne tik siaurai incidentų nišai,
   kurioje veikia visa DI SRE klasė.
3. **Veiksmų vykdymas su žmogaus patvirtinimais** – konkurentų agentai arba tik
   skaito („Cleric", „HolmesGPT"), arba vykdo be lygiaverčio konteksto lygmens
   patikimumo modelio; „NAVA" derina realų vykdymą su patvirtinimų dėžute ir
   auditu.
4. **Prieiga už ugniasienės per atvirkštinius tunelius – ir į Kubernetes, ir į
   paprastus serverius.** Dalis uždarų tiekėjų turi išeinančius vykdiklius
   („Datadog Private Action Runner", „PagerDuty", „Kubiya"), tačiau nė vienas
   atvirojo kodo agentų projektas tunelių neturi apskritai, o **paprastų Linux
   serverių (ne Kubernetes) netaiko nė vienas apžvelgtas sprendimas** – visa
   rinka apsiriboja Kubernetes ir debesijos API. „NAVA" tuneliai aprėpia abu
   aplinkų tipus atviroje daugiaklientėje platformoje.
5. **Aplikacijų „auksinis kelias" toje pačioje sistemoje** – agentų klasė
   šablonų neturi, o šablonus turinčios IDP platformos neturi agentų; „NAVA"
   sujungia abu.
6. **Įėjimo barjeras ir kaina** – atviras branduolys nemokamas (plg. „Kubiya"
   nuo ~15 000 USD/metus ar „Resolve AI" individualią stambių įmonių
   kainodarą); pajamos generuojamos iš SaaS patogumo ir įmonėms skirtų
   funkcijų, todėl produktas prieinamas ir vidutinėms organizacijoms.

**Regioninis kontekstas.** Regione artimiausi žaidėjai yra gretutiniai, ne
tiesioginiai: „Cast AI" (Vilnius; vienaragis nuo 2026 01) siūlo uždarą SaaS
Kubernetes sąnaudų optimizavimo platformą su prijungtu „OpsPilot" DI SRE
agentu, tačiau yra registruota JAV, neturi nei atvirojo kodo, nei savarankiško
diegimo, o jos branduolys – sąnaudų optimizavimas, ne bendros paskirties
operacijos; „Oxylabs" (Vilnius) kuria žiniatinklio duomenų infrastruktūrą DI
agentams, ne operacijų platformą. **Atvirojo kodo, savarankiškai diegiamos, ES
jurisdikcijos DI agentų platformos infrastruktūros operacijoms nerasta nei
Baltijos regione, nei visoje ES** – artimiausias europinis sprendimas
(„Hyground", žr. aukščiau) yra uždaras ir vienos organizacijos licencijos
modelio. Šią suvereniteto nišą „NAVA" tiesiogiai taiko.

---

## 1.7. Nurodykite, kokio tipo modelius planuojate naudoti (komerciniai per API, atviro kodo diegiami savo infrastruktūroje, ar nuosavas modelis). Ar planuojama modelį pritaikyti (adaptuoti), naudojant savo duomenis, siekiant pagerinti tikslumą?

**Modelių politika – modelių atžvilgiu neutrali (angl. *model-agnostic*).** Tai
ne techninė detalė, o esminė produkto vertė: klientas pats pasirenka modelių
tiekėją, todėl nėra priklausomybės (angl. *vendor lock-in*) nei nuo vieno
vendoriaus kainodaros, nei nuo ne ES jurisdikcijos.

Palaikoma:

- **Komerciniai modeliai per API** – per „OpenAI"-suderinamą sąsają: „OpenAI GPT"
  šeima, „Anthropic Claude" (per suderinamą sąsają), „Mistral", „OpenRouter" ir
  kiti;
- **Atviro kodo modeliai, diegiami kliento infrastruktūroje** – „Llama",
  „Mistral", „Qwen" ir kt., paleisti per „vLLM", „Ollama" ar „LM Studio"; ta pati
  „OpenAI"-suderinama sąsaja;
- **Google Gemini** – projekto apimtyje numatytas nativus (angl. *native*)
  palaikymas.

**Nuosavas modelis nekuriamas ir nemokomas.** Kredencialus pateikia pats klientas;
suvereniteto reikalaujantiems klientams visa grandinė gali veikti tik su
vietiniais modeliais.

**Modelio pritaikymas perkvalifikuojant (angl. *fine-tuning*) NEPLANUOJAMAS** –
jis prieštarautų modelių neutralumo principui (pritaikytas modelis pririštų
klientą prie vieno konkretaus modelio) ir agentiniam sprendimui nėra reikalingas.
Vietoje to naudojami trys sluoksniai:

1. **Promptų inžinerija (angl. *prompt engineering*)** – persona, struktūrizuotos
   (JSON schema) įrankių apibrėžtys, konteksto lygmens patikimumo politika;
2. **Atgavimu papildytas generavimas (angl. *retrieval-augmented generation*,
   RAG) ir ilgalaikė atmintis** – kliento specifiniai duomenys įtraukiami į
   kontekstą **vykdymo metu**, ne per modelio treniravimą;
3. **Konteksto suspaudimas (angl. *compaction*)** – ilgi pokalbiai suspaudžiami
   atskiru, pigesniu modeliu.

---

## 1.8. Kokia konkreti modelio architektūra planuojama ir kokios technologijos ir (arba) programavimo kalbos bus naudojamos? Ar DI, blokų grandinės technologijų, robotikos procesų automatizavimo produktas ir (ar) sprendimas reikalauja realaus laiko apdorojimo?

**Architektūros principas.** Nekuriama nuosava neuroninio tinklo architektūra –
kuriama **agentinė vykdymo architektūra** aplink pasirenkamą LLM: „įrankių kilpa"
su struktūrizuotais įrankių iškvietimais, kontroliniais taškais (angl.
*checkpoint*; pertraukimas žmogaus patvirtinimui ir tęsimas), ilgalaike
atmintimi, konteksto suspaudimu ir sub-agentų delegavimu.

**Serverinė dalis (angl. *backend*) – „Go" (v1.26):**

| Komponentas | Technologija |
|---|---|
| Agentų vykdymo variklis | atvirojo kodo **„Eino"** karkasas (įrankių kilpa, srautinis generavimas, kontroliniai taškai) |
| Valdikliai (angl. *reconciler*) | **„controller-runtime"** ir mūsų pačių vystomas **„multicluster-runtime"** |
| Daugiaklientė valdymo plokštuma | atvirojo kodo **„kcp"** (Kubernetes-native darbo erdvių izoliacija) |
| Aplikacijų materializavimas | **„kro"** (angl. *Kube Resource Orchestrator*) resursų grafai iš šablonų |
| Patvarus duomenų sluoksnis | **PostgreSQL** (transkriptai, atmintis, auditas, naudojimo apskaita) |
| Užklausų sluoksniai | **GraphQL** (darbo erdvės resursai portalui) ir viso ūkio lygmens indeksas su ryšių traversavimu (skaitmeninis dvynys) |
| Agentų sąsaja išoriniams klientams | **Model Context Protocol (MCP)** serveris |

**Priekinė dalis (angl. *frontend*) – „Vue 3" + „TypeScript":** „Vite" (kūrimas ir
paketavimas), „Tailwind CSS" (stilius), „Pinia" (būsena), „urql" (GraphQL
klientas), „xterm.js" (interaktyvus terminalas), „Vue Router", „markdown-it".
Portalas kuriamas pagal **mikro-priekinių dalių** (angl. *micro-frontend*)
principą: kiekvienas tiekėjas pateikia savo sąsajos dalį, kurią pagrindinis
portalas automatiškai aptinka ir integruoja per vienos kilmės (angl.
*same-origin*) tarpinį serverį.

**Realaus laiko apdorojimo poreikis – dalinis, dviejų skirtingų pobūdžių:**

- **Interaktyvus pokalbis – taip.** Atsakymai generuojami srautu (angl.
  *streaming*, SSE), tikslinė reikšmė ≤ 5 s (p95, neįskaitant paties įrankio
  vykdymo trukmės).
- **Infrastruktūros būsena – ne griežtai sinchroninė.** Skaitmeninis dvynys
  atnaujinamas savitaise derinimo (angl. *reconcile*) kilpa, periodiškai
  persinchronizuojama, todėl būsena yra **„galiausiai nuosekli"** (angl.
  *eventually consistent*) su realia infrastruktūra. Tai sąmoningas
  architektūrinis pasirinkimas: praradus atskirą įvykį dėl tunelio trikties,
  kita persinchronizacija būseną atstato – įvykiais grįsti ETL sprendimai to
  negarantuoja.

---

## 1.9. Kokia DI, blokų grandinės technologijų, robotikos procesų automatizavimo infrastruktūra (pvz.: serveriai, GPU, debesijos paslaugos) ir kokie įrankiai (pvz.: tiekėjų API, on-edge apdorojimas) numatomi naudoti?

**Infrastruktūros pagrindas – standartinė debesijos ir serverinė
infrastruktūra.** Nuosavas modelis nekuriamas ir nemokomas (žr. 1.7), todėl
didelio našumo skaičiavimų (angl. *HPC*) ar GPU pajėgumų **modelių treniravimui
nereikia**. GPU infrastruktūra projekte naudojama **modelių serveriavimui**:
projekto apimtyje numatyta savarankiškai talpinamų atvirų modelių („Llama",
„Mistral", „Qwen" per „vLLM" / „Ollama") serveriavimo GPU pajėgumuose
integracija ir našumo optimizavimas (žr. 1.17, 3 etapas), kad suvereniteto
reikalaujantys klientai visą grandinę galėtų paleisti be išorinių API – savo
pačių arba nuomojamoje GPU bazėje. Platforma su visais modeliais – tiek
komerciniais, tiek vietiniais – bendrauja per tą pačią „OpenAI"-suderinamą
sąsają, todėl GPU sluoksnis yra pakeičiamas ir nesukuria priklausomybės nuo
vieno tiekėjo.

Konkrečiai naudojama:

- **Valdymo plokštuma ir tiekėjai** – Kubernetes klasteris (SaaS atveju –
  valdomas debesijos klasteris; savarankiško diegimo atveju – kliento);
- **Duomenų sluoksnis** – PostgreSQL;
- **Aplikacijų vykdymo aplinka** – Kubernetes klasteris, kuriame
  materializuojamos iš šablonų sukurtos aplikacijos;
- **Modelių serveriavimas (pasirinktinai)** – GPU mazgai (debesijos arba
  kliento), kuriuose per „vLLM" / „Ollama" talpinami atviri modeliai;
- **Prijungtos kliento aplinkos** – kliento Kubernetes klasteriai, serveriai ir
  pakraščio (angl. *edge*) įrenginiai, pasiekiami per atvirkštinius tunelius
  (mums infrastruktūros kaštų nesukuria).

**Įrankiai (agentų pusėje):** LLM tiekėjų API arba savarankiškai talpinami
modeliai kliento pasirinkimu; saityno paieška ir turinio nuskaitymas su apsauga
nuo serverio pusės užklausų klastojimo (angl. *Server-Side Request Forgery*,
SSRF); „GitHub"; savavališkos išorinės sistemos per MCP; infrastruktūros
operacijos per atvirkštinius tunelius; viso ūkio užklausos per skaitmeninio
dvynio indeksą; pokalbių kanalai („Telegram", „Slack", „Discord", SMTP).

---

## 1.10. Koks duomenų kiekis reikalingas pradiniam mokymui ir tolesniam palaikymui? Kaip bus sprendžiami modelio šališkumo klausimai? Koks planuojamas modelio atnaujinimo dažnis ir ar numatytas A/B testavimas?

**Pradinio mokymo duomenys – netaikoma.** Projekte nevykdomas nuosavo modelio
treniravimas ar perkvalifikavimas; naudojami tik trečiųjų šalių LLM per API arba
savarankiškai talpinami atviri modeliai (taip pat per API). Kliento duomenys į
modelių apmokymą nepatenka.

**Palaikymui reikalingi duomenys** – tik vykdymo metu susidarantys (žr. 1.11):
pokalbių transkriptai, agento atmintis, skaitmeninio dvynio indeksas ir audito
žurnalas. Jie naudojami kontekstui, ne mokymui.

**Šališkumo (angl. *bias*) valdymas.** Modelio vidinis šališkumas paveldimas iš
tiekėjo; mes jį valdome trimis būdais:

1. **Grindimas realiais duomenimis (angl. *grounding*)** – agentas veiksmus
   grindžia konkrečiais, patikrinamais įrankių stebėjimais, o ne laisvai
   generuojamu turiniu; būsenos prasimanyti negali;
2. **Laisvas modelio pasirinkimas** – klientas gali pakeisti modelį ar tiekėją,
   jei rezultatai netenkina;
3. **Žmogaus kontrolė** – rizikingi veiksmai reikalauja patvirtinimo, o
   neprižiūrimi paleidimai neturi rašymo teisių.

**Atnaujinimo dažnis.** Tiesioginis perkvalifikavimas nevykdomas. Vietoje jo
veikia **prompto ir vertinimo iteracijos ciklas**: prieš kiekvieną sisteminių
promptų, įrankių apibrėžčių ar modelio pakeitimą paleidžiamas automatizuotas
vertinimo rinkinys (angl. *evaluation harness*) – kuruotų užduočių aibė.
Naujesnės modelių versijos integruojamos ir pervertinamos tuo pačiu rinkiniu.

**A/B testavimas – planuojamas su sąlyga.** Modelių profilių architektūra
(skirtingi modeliai pokalbiui, foninėms užduotims ir konteksto suspaudimui)
leis tą pačią užduotį paleisti su skirtingais modeliais ar promptais ir
palyginti. Formalus A/B mechanizmas bus įgyvendintas tik jei vertinimo rinkinio
rezultatai parodys, kad jo reikia.

**Tikslinės kokybės reikšmės.** Vertinimo rinkinys yra projekto apimties darbas
(2–4 etapai; žr. 1.17); jo tikslinės – dar neišmatuotos – reikšmės: užduoties
įvykdymo dažnis ≥ 85 %, įrankio iškvietimo tikslumas ≥ 90 %, atsakymo
pagrįstumas ≥ 95 % (halucinacijų ≤ 5 %), rizikingų veiksmų eskalavimo žmogui
tikslumas ≥ 95 %. Parengus rinkinį (2 etapas), šios reikšmės matuojamos prieš
kiekvieną promptų, įrankių ar modelio pakeitimą.

---

## 1.11. Pateikite pagrindinių turimų duomenų rinkinių aprašymą. Kiekvienam rinkiniui nurodykite:

**1.11.1.** tipą ir formatą (pvz.: struktūruoti, nestruktūruoti, tekstas, vaizdai, garsas);
**1.11.2.** šaltinį (vidinė CRM sistema, išorinis partneris, Jūsų nuosavybė ir pan.);
**1.11.3.** kiekį (pvz.: 1.5 mln. įrašų; 50,000 nuotraukų; 5 TB);
**1.11.4.** laikotarpį, kurį duomenys apima.

**Projektas neremiasi iš anksto turimu duomenų rinkiniu** – nei modelio mokymui,
nei paleidimui. Nėra istorinio rinkinio, kurį reikėtų įsigyti ar paruošti;
sistema pradeda veikti tuščia ir kaupia duomenis eksploatacijos metu.

Vykdymo metu susidarantys duomenys:

| Duomenų tipas | 1.11.1 Tipas ir formatas | 1.11.2 Šaltinis | 1.11.3 Kiekis | 1.11.4 Laikotarpis |
|---|---|---|---|---|
| **Pokalbių transkriptai** | nestruktūruotas tekstas ir struktūrizuoti įrankių iškvietimai; JSON, PostgreSQL | sukuriama platformoje (naudotojo ir agento sąveika) | KB eilės vienam pokalbiui | nuo paskyros sukūrimo; saugojimo terminas nustatomas kliento politika |
| **Agento ilgalaikė atmintis** | struktūrizuoti trumpi teksto įrašai (iki ~8 KB); PostgreSQL | agento generuojama vykdymo metu | dešimtys–šimtai įrašų vienam agentui | agento gyvavimo laikotarpis |
| **Skaitmeninio dvynio indeksas** | struktūruoti serializuoti infrastruktūros objektai ir jų ryšiai; JSON/JSONB, PostgreSQL arba SQLite | prijungtos kliento aplinkos (sinchronizuojama per tunelius) | priklauso nuo kliento infrastruktūros dydžio; tūkstančiai–dešimtys tūkstančių objektų vienai vidutinei aplinkai | esamos būsenos momentinis vaizdas, nuolat atnaujinamas (ne istorinis archyvas) |
| **Audito žurnalas** | struktūrizuoti įrašai (agentas, paleidimas, įrankis, argumentų santrauka, rezultatas, trukmė); PostgreSQL | platformos vykdymo variklis | auga tiesiškai su naudojimu | nuo paskyros sukūrimo |
| **Naudojimo apskaita** | struktūrizuoti skaitikliai (žetonai, sąnaudos, objektų kiekiai); PostgreSQL | platformos apskaitos sluoksnis | agreguota pagal laikotarpį – apimtis kukli | nuo paskyros sukūrimo |
| **Konfigūracija** | persona, politika, įrankių apibrėžtys; tekstas arba JSON schema | naudotojo įvedama | KB eilės vienam agentui | agento gyvavimo laikotarpis |

Visi rinkiniai – **mūsų arba kliento nuosavybė** (priklausomai nuo diegimo
modelio; žr. 1.14), izoliuoti per klientą, teksto pobūdžio ir kuklios apimties.
Išoriniai partneriai duomenų netiekia.

---

## 1.12. Ar duomenys bus sužymėti (angl. labeled)? Jei taip, aprašykite žymėjimo procesą ir kokybę. Jei duomenys nesužymėti, detalizuokite, kaip planuojate juos parengti DI modelio mokymui, jeigu tai yra būtina.

**Netaikoma modelio mokymui** – projekte nevykdomas nuosavo modelio treniravimas
ir nekuriamas žymėtų (angl. *labeled*) duomenų rinkinys, todėl žymėjimo proceso
nėra ir jo nereikia.

**RAG ir atminties sluoksnyje** naudojami duomenys (agento atmintis, ankstesni
pokalbiai, skaitmeninio dvynio objektai) formaliai nežymimi. Vietoje žymėjimo
naudojama:

- **struktūra vietoje anotacijos** – infrastruktūros objektai jau yra
  struktūrizuoti (tipas, ryšiai, žymės), todėl atgavimas vyksta pagal tipą ir
  ryšius, ne pagal rankinį žymėjimą;
- **agento pačio kuriama atmintis** – agentas įrašus rašo su antrašte ir
  kontekstu, t. y. žymėjimas vyksta automatiškai vykdymo metu;
- **atgavimas pagal aktualumą** – į modelio kontekstą patenka tik
  relevantiškiausi įrašai.

Kokybės kontrolė vykdoma ne per žymėjimą, o per **automatizuotą vertinimo
rinkinį** (žr. 1.10) – kuruotą užduočių aibę, kuria matuojamas galutinis agento
elgesys, ne tarpinių duomenų anotacija.

---

## 1.13. Kokia bus duomenų kokybė (ar išsamūs, ar reikia juos valyti, padaryti anoniminius)? Ar projektui įgyvendinti reikės papildomų duomenų (įsigijimas, vieši rinkiniai) ir kaip jie bus integruojami?

**Kokybė.** Duomenys yra teksto pobūdžio, kuklios apimties ir izoliuoti per
klientą. Struktūrizuota jų dalis (skaitmeninio dvynio objektai) ateina tiesiai iš
Kubernetes API, todėl yra schema apibrėžta ir nereikalauja valymo.
Nestruktūrizuota dalis (pokalbiai) yra pirminis įrašas – jo „valyti" negalima,
nes tai ir yra faktinis įvykių žurnalas.

**Išsamumas.** Skaitmeninio dvynio išsamumą užtikrina savitaisė derinimo kilpa –
periodinė persinchronizacija atstato būseną net praradus atskirą įvykį. Tai
struktūrinis atsakas į duomenų pilnumo riziką.

**Anonimizavimas.** Taikomas duomenų minimizavimas: renkama tik tai, ko reikia
konkrečiai užduočiai. Numatoma jautrių reikšmių (kredencialų, žetonų) **maskavimo
(angl. *redaction*) įrankių argumentuose ir žurnaluose** funkcija – tai projekto
apimties darbas. Visiškas anonimizavimas netaikomas ir netikslingas: audito
žurnalas be veiksmo autoriaus prarastų savo paskirtį; vietoje to naudojama
prieigos kontrolė, izoliacija ir šifravimas (žr. 1.14).

**Papildomi duomenys.** Papildomų duomenų rinkinių įsigijimas ar viešų rinkinių
integravimas **neplanuojamas** – produkto kokybė priklauso ne nuo duomenų kiekio,
o nuo agentinės architektūros ir realių įrankių stebėjimų tikslumo. Vertinimo
rinkinys bus sukurtas mūsų pačių kaip kuruota užduočių aibė, o ne įsigytas.

---

## 1.14. Ar tarp planuojamų naudoti duomenų bus asmeninių, konfidencialių ar kitų jautrių duomenų? Kaip užtikrinsite atitiktį 2016 m. balandžio 27 d. Europos Parlamento ir Tarybos reglamento (ES) 2016/679 dėl fizinių asmenų apsaugos tvarkant asmens duomenis ir dėl laisvo tokių duomenų judėjimo ir kuriuo panaikinama Direktyva 95/46/EB (Bendrojo duomenų apsaugos reglamento)/2024 m. birželio 13 d. Europos Parlamento ir Tarybos reglamento (ES) 2024/1689, kuriuo nustatomos suderintos dirbtinio intelekto taisyklės ir iš dalies keičiami reglamentai (EB) Nr. 300/2008, (ES) Nr. 167/2013, (ES) Nr. 168/2013, (ES) 2018/858, (ES) 2018/1139 ir (ES) 2019/2144 ir direktyvos 2014/90/ES, (ES) 2016/797 ir (ES) 2020/1828 (Dirbtinio intelekto akto) reikalavimams ir kaip bus tvarkomi jautrūs duomenys?

**Ar bus asmens ar jautrių duomenų? Taip, potencialiai.** Konkrečiai:

- **audito žurnaluose ir telemetrijoje** – naudotojų identifikatoriai, el. pašto
  adresai, IP adresai, veiksmų autoriai;
- **pokalbių transkriptuose ir agento atmintyje** – bet kokie naudotojo įvesti
  duomenys, kurių turinio iš anksto apriboti negalime;
- **kredencialuose** – kliento prieigos raktai prie jo paties sistemų (saugomi
  kaip paslaptys, niekada nepatenka į modelio kontekstą kaip tekstas).

Specialių kategorijų (BDAR 9 str.) duomenų tvarkymas nenumatomas ir produkto
paskirčiai nereikalingas.

**BDAR atitiktis – priklauso nuo diegimo modelio:**

| Modelis | Vaidmuo | Garantija |
|---|---|---|
| **Savarankiškas diegimas** | klientas – **duomenų valdytojas**; mūsų programinė įranga duomenų nemato | stipriausia – duomenys niekada nepalieka kliento infrastruktūros |
| **SaaS** | veikiame kaip **duomenų tvarkytojas** pagal duomenų tvarkymo sutartį (angl. *Data Processing Agreement*, DPA) | sutartinė ir techninė |

**Techninės ir organizacinės priemonės (abiem atvejais):** duomenų
minimizavimas (surinkimas pagal poreikį, ne nuolatinis viso srauto kopijavimas);
**daugiaklientė izoliacija darbo erdvių lygmeniu** („kcp"), kur kiekvienas
klientas turi atskirą loginę valdymo plokštumą; šifravimas ramybės būsenoje ir
perdavimo metu; **OIDC** autentifikacija ir vaidmenimis grįsta prieigos kontrolė
(angl. *role-based access control*, RBAC); auditas; duomenų subjektų teisių
įgyvendinimas (prieiga, ištrynimas); **jokio antrinio naudojimo** – duomenys
nenaudojami modelių apmokymui nei mūsų, nei modelių tiekėjų.

**ES DI aktas (Reglamentas (ES) 2024/1689).** Pareiškėjo vertinimu, „NAVA"
standartiškai **nepatenka į didelės rizikos kategoriją** pagal III priedą:

- produktas yra **horizontali IT operacijų automatizavimo priemonė**, neskirta
  III priede išvardytoms sritims (biometrija, įdarbinimas, švietimas, esminės
  paslaugos, teisėsauga, migracija, teisingumas);
- rizika mažinama **žmogaus priežiūra pagal dizainą** ir **ribotu autonomiškumu**
  – neprižiūrimi paleidimai neturi rašymo teisių;
- **bendrosios paskirties DI (angl. *general-purpose AI*, GPAI) tiekėjo prievolės
  tenka modelio tiekėjui** („OpenAI", „Google" ir kt.), o ne „NAVA" kaip
  įrankiui.

**Skaidrumo prievolės** (52 str.) laikomasi: naudotojas visada aiškiai mato, kad
bendrauja su DI agentu, ir mato kiekvieną jo atliktą veiksmą.

**Išlyga.** Galutinį rizikos lygį lemia **kliento naudojimo kontekstas**. Jei
klientas platformą naudotų didelės rizikos srityje, tam konkrečiam diegimui
galėtų atsirasti papildomų prievolių. Todėl teiksime naudojimo gaires, o žmogaus
priežiūros ir audito mechanizmai suteikia klientui techninį pagrindą savo
prievolėms įvykdyti. **Šis vertinimas yra pareiškėjo pozicija, o ne nepriklausomo
teisininko išvada**; prieš komercinį paleidimą numatoma teisinė peržiūra.

---

## 1.15. Nurodykite, kokio tipo modelius planuojate naudoti (komerciniai per API, atviro kodo diegiami savo infrastruktūroje ar nuosavas modelis). Ar planuojama modelį pritaikyti (adaptuoti) naudojant savo duomenis, siekiant pagerinti tikslumą?

Modelių pasirinkimo politika sutampa su **1.7 punktu** (komerciniai per API arba
atviro kodo, diegiami kliento infrastruktūroje; nuosavas modelis nekuriamas;
pritaikymas perkvalifikuojant, angl. *fine-tuning*, neplanuojamas).

Papildomai – **modelių profilių mechanizmas.** Kiekvienam agentui priskiriami
atskiri modeliai pagal paskirtį:

| Profilis | Paskirtis | Tipinis pasirinkimas |
|---|---|---|
| „chat" (pokalbis) | interaktyvus pokalbis, planavimas, įrankių iškvietimai | stipriausias (ir brangiausias) modelis |
| „background" (foninės užduotys) | foninės ir suplanuotos užduotys, „širdies plakimai" | pigesnis arba vietinis modelis |
| „compaction" (suspaudimas) | ilgo pokalbio suspaudimas į santrauką | pigus modelis |

Praktinė nauda dvejopa: **sąnaudų valdymas** (didžioji dalis paleidimų yra
foniniai ir jiems nereikia stipriausio modelio) ir **suverenitetas pagal
jautrumą** – klientas gali nurodyti, kad foninės užduotys su jautriais duomenimis
vyktų tik vietiniame modelyje, o pokalbiui naudoti komercinį.

---

## 1.16. Kokia konkreti modelio architektūra planuojama ir kokios technologijos ir (arba) programavimo kalbos bus naudojamos? Ar DI, blokų grandinės technologijų, robotikos procesų automatizavimo produktą ir (ar) sprendimą reikės apdoroti realiuoju laiku?

Architektūros ir technologijų pasirinkimas sutampa su **1.8 punktu**.

Papildomai, dėl realaus laiko:

- **Srautinis (angl. *streaming*) generavimas** – agentų vykdymo variklis atsakymą
  ir įrankių iškvietimus perduoda naudotojui dalimis, jam nelaukiant viso
  rezultato; portale realiu laiku matomas kiekvienas agento žingsnis.
- **Kontroliniai taškai (angl. *checkpoint*)** – paleidimas gali būti
  **sustabdytas** laukiant žmogaus patvirtinimo ir vėliau **atnaujintas** nuo tos
  pačios vietos, neprarandant konteksto. Tai leidžia agentui saugiai veikti
  valandas ar dienas.
- **Skaitmeninis dvynys** nėra griežtai sinchroninis – jis savitaisis ir
  „galiausiai nuoseklus" (žr. 1.8). Šis pasirinkimas yra atsparumo, o ne
  kompromiso klausimas.
- **Fono vykdymas nepriklauso nuo naudotojo sesijos** – tvarkaraščiai ir
  trigeriai suveikia net kai nė vienas naudotojas neprisijungęs.

---

## 1.17. Nurodyti užduotis ir veiksmus pereinant nuo MVP iki išvystyto DI, blokų grandinės technologijų, robotikos procesų automatizavimo produkto ir (ar) sprendimo (parengto rinkai).

**Startinė pozicija (TRL 3–4).** Projektas pradedamas nuo patvirtintos
koncepcijos: pasirinktas technologinis stekas (žr. 1.8), suprojektuota
tiekėjų architektūra ir prototipais patikrintos kritinės techninės prielaidos
(agentinė įrankių kilpa, daugiaklientė izoliacija, atvirkštinių tunelių
prieiga). **MVP – veikiantis produkto branduolys – sukuriamas 1–2 etapuose**,
o 3–4 etapai jį išvysto iki rinkai parengto produkto (TRL 8–9): pridedamas
„auksinis kelias", skaitmeninis dvynys, apskaita, saugumo kietinimas ir
pilotiniai diegimai.

**Darbų planas (12 mėn., 4 etapai po 3 mėn.):**

**1 etapas (1–3 mėn.) – produkto branduolio sukūrimas.**
- Agentų vykdymo variklio (įrankių kilpa, kontroliniai taškai) ir
  daugiaklientės izoliacijos („kcp" darbo erdvės, tiekėjų mechanizmas)
  sukūrimas;
- Saugios prieigos sluoksnio (atvirkštiniai tuneliai, OIDC) sukūrimas;
- Patvaraus duomenų sluoksnio (transkriptai, atmintis, auditas) sukūrimas,
  įskaitant šifravimą ramybės būsenoje;
- Pilnas sistemos paleidimas realioje aplinkoje (angl. *end-to-end*) ir
  integracinių defektų šalinimas.

*Etapo rezultatas:* agentas per portalą įvykdo daugiapakopę užduotį realioje
prijungtoje aplinkoje.

**2 etapas (3–6 mėn.) – autonomija, įrankiai ir kokybės matavimas (MVP
užbaigimas).**
- Autonomijos posistemio sukūrimas: tvarkaraščiai, „širdies plakimai",
  įvykių trigeriai su filtrais ir dubliavimo apsauga;
- Įrankių šeimos: saitynas, „GitHub", MCP, failų darbo erdvė, infrastruktūros
  operacijos;
- Konteksto lygmens patikimumo modelis ir patvirtinimų dėžutė su atnaujinamais
  (angl. *checkpoint*) paleidimais;
- **Automatizuotas vertinimo rinkinys** – kokybės matavimo pagrindas.

*Etapo rezultatas:* MVP – agentai autonomiškai veikia pagal tvarkaraščius ir
įvykius su patvirtinimų kontrole; kokybė matuojama vertinimo rinkiniu (1.10
tikslinės reikšmės).

**3 etapas (6–9 mėn.) – „auksinis kelias" ir skaitmeninis dvynys.**
- Aplikacijų šablonų posistemis: šablonų schema, kūrimo režimas, kodo saugykla
  kaip tiesos šaltinis, importas iš esamų saugyklų;
- Netechninio vartotojo kelias „nuo pokalbio iki veikiančios aplikacijos";
- Skaitmeninio dvynio užklausų sluoksnio ir portalo vizualizacijų sukūrimas;
- Savarankiškai talpinamų modelių serveriavimo (GPU, „vLLM" / „Ollama")
  integracija ir našumo optimizavimas;
- Naudojimo apskaitos ir kvotų posistemis.

*Etapo rezultatas:* netechninis vartotojas savarankiškai sukuria ir paleidžia
aplikaciją iš šablono; viso ūkio užklausos veikia per skaitmeninį dvynį.

**4 etapas (9–12 mėn.) – komercinė parengtis, sauga ir pilotai.**
- Biudžetų, audito, šifravimo ir saugumo kietinimas; nepriklausoma saugumo
  peržiūra;
- Kanalų ir „OAuth" integracijų užbaigimas;
- **1–3 pilotiniai diegimai** su realiais klientais ir grįžtamojo ryšio
  įgyvendinimas;
- Dokumentacija, diegimo paketai („Helm"), prenumeratos ir apmokestinimo
  posistemis, komercinis paleidimas.

*Etapo rezultatas:* komerciškai parengtas produktas (TRL 8–9) su 1–3
pilotiniais diegimais, dokumentacija ir patvirtintu kainynu.

**Rizikų valdymas:**

| Rizika | Tikimybė / poveikis | Valdymo priemonė |
|---|---|---|
| Priklausomybė nuo LLM tiekėjų kainodaros ar prieinamumo | vidutinė / didelis | modelių neutralumas – keičiami tiekėjai, vietiniai modeliai (žr. 1.7) |
| LLM sąnaudų augimas eksploatacijoje | vidutinė / vidutinis | biudžetai ir kvotos kiekvienam agentui; pigesni modeliai foninėms užduotims (žr. 1.15) |
| Netiesioginė komandų injekcija / agento klaida su rašymo teisėmis | žema / didelis | konteksto lygmens patikimumo modelis, patvirtinimų dėžutė, auditas; nepriklausoma saugumo peržiūra 4 etape |
| Integraciniai defektai jungiant komponentus | vidutinė / vidutinis | pilnas „end-to-end" paleidimas jau 1 etape; nuolatinė integracija |
| Pilotinių klientų pritraukimas vėluoja | vidutinė / vidutinis | atviro kodo bendruomenė kaip pardavimų piltuvėlis; MSP partnerių kanalas; ankstyvas viešinimas |
| Agentinių DI projektų nusivylimo banga rinkoje („Gartner": >40 % agentinių DI projektų bus nutraukta iki 2027 m. pabaigos dėl sąnaudų ir neaiškios vertės) | vidutinė / vidutinis | vertė matuojama vertinimo rinkiniu ir pilotų metrikomis (1.4, 1.10); sąnaudos valdomos kvotomis ir biudžetais; žmogaus patvirtinimų dizainas mažina brangių klaidų riziką |

---

## 1.18. Aprašyti sukurto DI, blokų grandinės technologijų, robotikos procesų automatizavimo produkto ir (ar) sprendimo paleidimo į rinką etapus ir komercinimo strategiją.

**Įėjimo į rinką etapai:**

1. **Pilotai projekto metu (6–12 mėn.)** – 1–3 klientai, dirbantys su mumis
   glaudžiai; tikslas ne pajamos, o produkto patvirtinimas realiomis sąlygomis
   ir matuojamos vertės įrodymas (žr. 1.4). Pilotai pritraukiami per esamą
   profesinį tinklą (DevOps ir platformų bendruomenė, MSP partneriai, ilgamečiai
   komandos klientai) ir atviro kodo bendruomenę; potencialių pilotinių klientų
   ketinimų raštai (angl. *letter of intent*, LOI) – [pridėti kaip paraiškos
   priedą].
2. **Atviro kodo bendruomenė lygiagrečiai** – produktas nuo pirmos dienos
   viešas; savarankiškai diegiantys naudotojai yra ir kokybės grįžtamasis ryšys,
   ir pardavimų piltuvėlio viršus.
3. **Mokamos SaaS prenumeratos (nuo 12 mėn.)** – komercinis paleidimas su
   patvirtintu kainynu.
4. **Tarptautinė plėtra ES rinkoje** – tiesioginiai pardavimai ir partnerių
   (MSP) kanalas; MSP ypač aktualūs, nes jie perparduoda paslaugą toliau
   savo klientams.

**Komercinimo modelis – atviro branduolio (angl. *open core*):**

- **Branduolys – atvirojo kodo**, laisvai diegiamas kliento infrastruktūroje.
  Planuojama branduolio licencija – **„Apache 2.0"** (suderinama su naudojamu
  „kcp" karkasu ir CNCF ekosistema, todėl nekliudo trečiųjų šalių tiekėjams);
  įmonėms skirtos funkcijos platinamos pagal komercinę licenciją, o prekės
  ženklas registruojamas patvirtinus galutinį pavadinimą. Atviras branduolys –
  ne altruizmas, o įėjimo barjero mažinimas ir pasitikėjimo prielaida:
  suvereniteto reikalaujantys klientai (viešasis sektorius, reguliuojamos
  pramonės šakos) nesirenka produkto, kurio negali audituoti.
- **Pajamų šaltinis 1 – valdoma SaaS paslauga.** Prenumerata **kvotų principu**:
  kaina priklauso nuo naudojamų projektų, aktyvių DI agentų ir suvartojamų
  resursų kiekio (pvz., iki 5 projektų arba 10 agentų – vienas planas, daugiau –
  kitas). Kvotų ir apskaitos posistemis yra sudėtinė produkto dalis, todėl
  kainodara techniškai įgyvendinama, o ne deklaratyvi.
- **Pajamų šaltinis 2 – įmonėms skirtos funkcijos ir palaikymas** savarankiškai
  diegiantiems klientams (paslaugos lygio sutartys, angl. *SLA*, saugumo
  funkcijos, diegimo pagalba).
- **Pajamų šaltinis 3 (ilgalaikis) – tiekėjų ekosistema:** valdomas tiekėjų
  tiekimas, sertifikavimas ir palaikymas. Šis šaltinis auga sparčiau nei mūsų
  pačių kuriamų funkcijų kiekis, nes tiekėjus kuria ir trečiosios šalys.

**Kainynas.** Galutinis kainynas bus patvirtintas po pilotinio paleidimo, kai
turėsime realius naudojimo ir sąnaudų duomenis. Tikslinė SaaS prenumeratos
pajamų projekcija pirmaisiais metais po paleidimo – **apie 24 000 EUR**
(vertinimas, ne sutartinis įsipareigojimas); šio etapo tikslas yra padengti
operacines (infrastruktūros ir modelių serveriavimo) išlaidas, kad būtų galima
tęsti vystymą, o ne pasiekti pelningumą.

**Finansinės projekcijos (tikslinės, pilotais tikslintinos):**

| Metai po komercinio paleidimo | Mokančių klientų tikslas | Vidutinė metinė prenumerata | Tikslinės pajamos |
|---|---|---|---|
| 1 metai | 4–6 | ~4 000–6 000 EUR | ~24 000 EUR (SaaS) |
| 2 metai | ~15 | ~5 500 EUR | ~85 000 EUR (SaaS + pirmosios palaikymo sutartys) |
| 3 metai | ~35 | ~6 500 EUR | ~230 000 EUR (SaaS + įmonių funkcijos ir palaikymas) |

Prielaidos: kvotų principo kainodara (žr. aukščiau); klientų augimą varo atviro
kodo bendruomenė kaip pardavimų piltuvėlis ir MSP kanalas, kuriame vienas
partneris atstovauja keliems galutiniams klientams. Projekcijos konservatyvios,
palyginti su 1.6 nurodytu rinkos augimu (~46 % per metus): 3 metų tikslas
atitinka vos kelias dešimtis klientų ES rinkoje, kurioje veikia tūkstančiai
MSP ir vidutinių IT organizacijų.

---

## 1.19. Nurodyti ir aprašyti, ar kursite savo UX ir teiksite B2C ar B2B, ar teiksite SaaS paslaugą ir pan.

**Segmentas – B2B.** Tikslinė rinka yra organizacijos: DevOps ir platformų
komandos, MSP, vidutinės IT organizacijos. Pavieniams vartotojams (B2C) produktas
neskirtas – jo vertė atsiranda ten, kur yra bendra infrastruktūra, komandos ir
saugumo reikalavimai. Tačiau **vartotojų grupė organizacijos viduje yra
platesnė nei inžinieriai**: šablonai ir agentai skirti ir netechninėms produkto
bei verslo komandoms.

**Nuosavas UX – taip, kuriame patys.** Pagrindinė sąsaja yra žiniatinklio
portalas („Vue 3" + „TypeScript"), veikiantis kaip **bendra apvalkalo (angl.
*shell*) sąsaja**: kiekvienas tiekėjas įsijungia į ją savo mikro-priekine dalimi,
o naudotojas mato vieną nuoseklią programą, ne atskirų įrankių rinkinį.

Pagrindiniai UX srautai:

- **Agento kūrimas** – persona, modelių profiliai, įrankių rinkinys, elgsenos
  politika, biudžetas;
- **Pokalbis su agentu** – natūralios kalbos sąsaja su realiu laiku matomu
  planavimu, įrankių iškvietimais ir jų rezultatais;
- **Patvirtinimų dėžutė** – bendra visų agentų eilė, kurioje žmogus patvirtina
  arba atmeta rizikingus veiksmus ir atsako į agento klausimus;
- **Autonomija** – tvarkaraščiai, periodiniai patikrinimai, įvykių trigeriai;
- **Aplikacijų kūrimas iš šablonų** – savitarnos srautas netechniniam vartotojui;
- **Paleidimų ir audito peržiūra** – istorija, transkriptai, veiksmai, sąnaudos;
- **Aplinkų prijungimas** – vienos komandos registracija ir agento įdiegimas
  kliento aplinkoje;
- **Nustatymai** – modelių kredencialai, kanalai, biudžetai, kvotos.

**UX principas – žmogaus kontrolė pagal dizainą:** naudotojas visada mato, ką
agentas daro, gali įsiterpti bet kuriame žingsnyje ir mato sąnaudas.

**Kalbos ir prieinamumas.** Agentų bendravimas palaikomas lietuvių ir anglų
kalbomis (LLM yra natūraliai daugiakalbiai; portalo sąsajos lokalizacija –
projekto apimtyje), o portalo prieinamumas užtikrinamas vadovaujantis WCAG 2.1
AA gairėmis.

**Sąveikos kanalai (be portalo):** pokalbių kanalai („Telegram", „Slack",
„Discord"), el. paštas išeinantiems pranešimams, **MCP** (agentai pasiekiami iš
išorinių DI klientų) ir **komandinės eilutės įrankis** (angl. *CLI*)
inžinieriams.

**Paslaugos teikimo modelis – abu:**

| Modelis | Kam | Ypatumai |
|---|---|---|
| **SaaS** (valdoma paslauga) | daugumai klientų, greitas startas | prenumerata kvotų principu; mes valdome infrastruktūrą |
| **Savarankiškas diegimas** (angl. *self-hosted*, „Helm") | suvereniteto reikalaujantiems klientams | duomenys neišeina iš kliento infrastruktūros; pajamos iš įmonėms skirtų funkcijų ir palaikymo |

---

## Šaltiniai

Konkurentų ir rinkos analizė atlikta 2026 m. liepos mėn.; išsamus tyrimas su
visais šaltiniais – [`nava-research-2026-07.md`](./nava-research-2026-07.md).
Pagrindiniai šaltiniai pagal temą:

**Rinkos dydis ir prognozės (1.4, 1.6):**

1. „MarketsandMarkets", *AI Agents Market* (7,84 mlrd. USD 2025 → 52,62 mlrd. USD 2030) — <https://www.marketsandmarkets.com/Market-Reports/ai-agents-market-15761548.html>
2. „Gartner": 40 % įmonių programų su užduočių DI agentais iki 2026 m. pabaigos — <https://www.gartner.com/en/newsroom/press-releases/2025-08-26-gartner-predicts-40-percent-of-enterprise-apps-will-feature-task-specific-ai-agents-by-2026-up-from-less-than-5-percent-in-2025>
3. „Gartner": suvereniteto debesijos išlaidos — 80 mlrd. USD pasaulyje 2026 m.; Europoje +83 % per metus — <https://www.gartner.com/en/newsroom/press-releases/2026-02-09-gartner-says-worldwide-sovereign-cloud-iaas-spending-will-total-us-dollars-80-billion-in-2026>
4. „Gartner": >40 % agentinių DI projektų bus nutraukta iki 2027 m. pabaigos (1.17 rizika) — <https://www.gartner.com/en/newsroom/press-releases/2025-06-25-gartner-predicts-over-40-percent-of-agentic-ai-projects-will-be-canceled-by-end-of-2027>
5. ES „Apply AI" strategija („pirk europietišką", atvirojo kodo DI viešajame sektoriuje) — <https://digital-strategy.ec.europa.eu/en/policies/apply-ai>
6. ES „Cloud and AI Development Act" — <https://digital-strategy.ec.europa.eu/en/policies/cloud-and-ai-development-act>

**Konkurentai — DI SRE ir operacijų agentai (1.5, 1.6):**

7. „Resolve AI": >190 mln. USD, 1,5 mlrd. USD vertinimas (2026 04) — <https://www.prnewswire.com/news-releases/resolve-ai-announces-series-a-extension-at-a-1-5b-valuation-and-launches-resolve-ai-labs-to-advance-ai-systems-for-complex-production-environments-302743888.html>; <https://techcrunch.com/2026/02/04/ai-sre-resolve-ai-confirms-125m-raise-unicorn-valuation/>
8. „Cleric": tik skaitymo režimas, vieša kainodara (2 000 USD/mėn., ~20 USD/tyrimas) — <https://cleric.ai/product>; <https://cleric.ai/pricing>
9. „Kubiya.ai": kainodara ir „Local Runner" architektūra — <https://www.kubiya.ai/pricing>; <https://docs.kubiya.ai/docs/local-runners/installation>
10. „Hyground": suverenus savarankiškai diegiamas DI SRE, €3 mln. („Partech"), „Deutsche Bahn" — <https://hyground.ai/>; <https://partechpartners.com/news/hyground-raises-3m-pre-seed-round-to-build-the-sovereign-sre-agent-for-enterprise-it-operatons>
11. „Microsoft Azure SRE Agent" (bendrai prieinamas 2026 03) — <https://techcommunity.microsoft.com/blog/appsonazureblog/announcing-general-availability-for-the-azure-sre-agent/4500682>
12. „Datadog Bits AI": SRE agentas (2025 12), autonominis taisymas (2026 06), „Private Action Runner" — <https://www.datadoghq.com/about/latest-news/press-releases/datadog-launches-bits-ai-sre-agent-to-resolve-incidents-faster/>; <https://www.datadoghq.com/blog/dash-2026-new-feature-roundup-keynote/>; <https://docs.datadoghq.com/actions/private_actions/>
13. „Aurora" („Arvo AI"), „Apache 2.0" — <https://github.com/Arvo-AI/aurora>

**Konkurentai — atvirojo kodo ekosistema ir platformos (1.6):**

14. „HolmesGPT" (CNCF „Sandbox", „Robusta" + „Microsoft") — <https://www.cncf.io/blog/2026/01/07/holmesgpt-agentic-troubleshooting-built-for-the-cloud-native-era/>; <https://github.com/HolmesGPT/holmesgpt>
15. „kagent" (CNCF „Sandbox", „Solo.io") — <https://kagent.dev/>; <https://www.cncf.io/projects/kagent/>
16. „n8n": „Sustainable Use License", 5,2 mlrd. USD vertinimas (SAP, 2026) — <https://docs.n8n.io/sustainable-use-license/>; <https://www.trendingtopics.eu/sap-bets-big-on-ai-invests-in-n8n-at-a-5-2-billion-valuation/>
17. „Port": 100 mln. USD agentinei inžinerijos platformai (2025 12) — <https://siliconangle.com/2025/12/11/port-nets-100m-turn-developer-portal-agentic-ai-hub/>; <https://docs.port.io/ai-interfaces/ai-agents/overview>
18. „Qovery AI Builder" (2026 05) — <https://www.qovery.com/blog/the-lovable-experience-enterprise-governance-your-infrastructure-we-built-it>

**Regioninis kontekstas (1.6):**

19. „Cast AI" – vienaragis (2026 01), „OpsPilot" DI SRE agentas — <https://www.lrt.lt/en/news-in-english/19/2805099/lithuania-s-fifth-unicorn-vilnius-based-cast-ai-crosses-1bn-valuation>; <https://cast.ai/blog/meet-opspilot-your-ai-sre-agent-built-into-cast-ai/>
20. „Oxylabs" – 3,6 mlrd. USD vertinimas (2026 07), agentų duomenų infrastruktūra — <https://www.eu-startups.com/2026/07/new-lithuanian-unicorn-oxylabs-ends-bootstrapped-streak-after-securing-e113-6-million-at-e3-1-billion-valuation/>

---

## Vidinė pastaba rengėjui (pašalinti prieš teikiant)

Anksčiau fiksuoti dokumentų rinkinio prieštaravimai išspręsti (2026-07-27):

- **HPC/GPU** – apimtis suvienodinta: GPU naudojamas savarankiškai talpinamų
  modelių **serveriavimui** (ne treniravimui) – žr. 1.9 ir 1.17 (3 etapas);
  atitinka „nava-projekto-aprasymas.md" §5–§6;
- **Kanalų sąrašas** – visuose dokumentuose suvienodintas į „Telegram / Slack /
  Discord / el. paštas + MCP"; „WhatsApp" neįgyvendintas ir nebeminimas;
- **„Auksinio kelio" linija** – įtraukta į „nava-projekto-aprasymas.md" §1 ir
  §3.9, todėl akcentai tarp dokumentų sutampa;
- **36 000 / 24 000 EUR** – visur formuluojami kaip tikslinės, pilotais
  tikrintinos reikšmės (1.4, 1.18);
- **Pavadinimas „NAVA"** – darbinis; išlyga pateikta dokumento pradžioje;
- **Parengties formuluotė** – produktas visuose dokumentuose pristatomas kaip
  projekto metu sukuriamas (startinė pozicija – TRL 3–4 koncepcija, žr. 1.17);
  nuorodos į jau veikiančią sistemą pašalintos iš viso rinkinio;
- **Konkurencijos ir tvarumo tyrimas** – 1.5–1.6 ir 1.17 rizikos atnaujintos
  pagal išsamų tyrimą „nava-research-2026-07.md" (šaltiniai su nuorodomis ten;
  svarbiausi pakeitimai: regioninė išlyga dėl „Cast AI", pridėti „Hyground",
  „Azure SRE Agent" / „Datadog Bits AI", patikslintas prieigos už ugniasienės
  pranašumas).

Liko atlikti prieš teikiant: užpildyti pareiškėjo laukus „[…]"
(„nava-projekto-aprasymas.md"); pridėti pilotinių klientų ketinimų raštus (LOI)
arba išimti nuorodą į priedą 1.18 punkte; patvirtinti branduolio licencijos
pasirinkimą („Apache 2.0" – žr. 1.18); jei teikimas nusikeltų keliais
mėnesiais – atnaujinti 1.6 konkurentų faktus (analizės data – 2026 m. liepa).
