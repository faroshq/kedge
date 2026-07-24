# NAVA — atsakymai į techninio klausimyno patikslinimus

> Atsakymai parengti pagal realią „NAVA" (kedge) architektūrą. Backend — Go 1.26 (agentų variklis ant Eino);
> frontend — Vue 3 + TypeScript + Vite + Tailwind; valdymo plokštuma — `kcp`; duomenys — PostgreSQL;
> užklausų sluoksnis — GraphQL (kuery); daugelio klasterių valdymas — `multicluster-runtime`.

---

## 1. Produktas ir architektūra

### 1.4. Vertės skaičiai (SaaS pajamos, sąnaudų mažinimas)

Vertė klientui matuojama sutaupytu kvalifikuoto darbuotojo darbo laiku. Konservatyviai vertinant, viena
„NAVA" agentų aplinka gali perimti pasikartojančias operacijas, prilygstančias ~1 etatui. Vertinant
vidutiniškai ~3 000 EUR/mėn. darbo sąnaudas, tai vienam verslo subjektui sudaro **~36 000 EUR/metus**
sutaupytų operacinių sąnaudų (be netiesioginės naudos — greitesnio incidentų sprendimo ir mažesnės
žmogiškųjų klaidų rizikos).

Produktas skirtas **ne plačiajai rinkai, o specifiniam klientų ratui** — verslo subjektams, kurie nori
naudotis DI privalumais, bet išlaikyti kontrolę, kaip naudojami jų duomenys (viešasis sektorius,
reguliuojamos pramonės šakos, technologijų įmonės su paskirstyta infrastruktūra).

**Kainodara — kvotų (angl. *quota*) principu:** kaina priklauso nuo naudojamų projektų ir aktyvių DI agentų
skaičiaus (pvz., iki 5 projektų / 10 agentų — vienas planas, daugiau — kitas). Galutinis kainynas bus
patvirtintas po pilotų. **Pirmų metų po paleidimo tikslas — padengti operacines (infrastruktūros ir modelių
serveriavimo) išlaidas, kad būtų galima tęsti vystymą; planuojamos SaaS prenumeratos pajamos — ~24 000 EUR
pirmaisiais metais.**

---

### 1.5. Kaip skirtingų šaltinių duomenys suvedami į vieną apdorojimo srautą

„NAVA" nesukuria vieno monolitinio duomenų sandėlio ar vienkrypčio ETL konvejerio. Duomenys iš skirtingų
šaltinių suvedami trijuose lygmenyse: (1) nutolusi būsena į vieningą modelį įtraukiama **Kubernetes
„reconcilerio" (derinimo kilpos) principu**, (2) šis modelis pateikiamas kaip vieningas **skaitmeninis
dvynys** per GraphQL užklausų sluoksnį, ir (3) galutinis suvedimas įvyksta **agento vykdymo variklyje**,
kuris pagal poreikį sujungia visus šaltinius sprendimui priimti.

**1) Nutolusių šaltinių įtraukimas — derinimo kilpa (reconcile loop).** Struktūrinė infrastruktūros būsena į
platformą patenka ne per vienkryptį įvykių srautą, o per deklaratyvų, lygmens iššaukiamą (angl.
*level-triggered*) valdiklių modelį:
- **Registracija.** Kiekviena prijungta kliento aplinka valdymo plokštumoje aprašoma kaip `ClusterAccess`
  objektas — prieigos taškas, pasiekiamas per išeinantį atvirkštinį tunelį (be VPN, be atidarytų prievadų).
  Valdiklis atlieka aplinkos API atradimą (angl. *discovery*) ir sugeneruoja jos schemą.
- **Stebėjimas.** Valdiklis kiekvienoje prijungtoje aplinkoje užsako resursų srautus (angl. *watch* /
  *informers*); bet koks objekto pasikeitimas nutolusioje aplinkoje iššaukia `Reconcile` iškvietimą.
- **Derinimas.** Kiekvienas iškvietimas palygina **stebimą** nutolusios aplinkos būseną su platformoje
  saugoma būsena ir jas suvienodina (indeksuoja / atnaujina serializuotą objektą, perskaičiuoja jo ryšius).
  Kilpa periodiškai persinchronizuojama, todėl yra **savitaisė** (angl. *self-healing*): net jei dėl tunelio
  trikčio prarandamas atskiras įvykis, kita persinchronizacija būseną atstato — dvynys lieka **galiausiai
  nuoseklus** (angl. *eventually consistent*) su realia infrastruktūra.
- **Daugelio klasterių valdymas.** Ta pati derinimo kilpa vienu metu taikoma daugeliui aplinkų per atvirojo
  kodo **`multicluster-runtime`** karkasą (vieną iš mūsų pačių vystomų projektų — žr. 9 sk.): vienas
  valdiklis fanuoja (angl. *fan-out*) į visus prijungtus klasterius, o ne paleidžia atskirą procesą kiekvienam.

**2) Vieningas skaitymo modelis — skaitmeninis dvynys.** Visi indeksuoti objektai (Kubernetes klasteriai,
serveriai, jų būsenos ir tarpusavio ryšiai) pateikiami per vieningą **GraphQL užklausų sluoksnį** (kuery).
Ryšiai tarp objektų — nuosavybė, priklausomybės, nuorodos, tarpklasterinis susiejimas — apskaičiuojami
**skaitymo metu** iš indeksuoto sluoksnio. Nepriklausomai nuo to, kurioje kliento aplinkoje objektas fiziškai
yra, agentas jį pasiekia per vieną autentifikuotą galinį tašką, adresuodamas pagal klasterio ID. Tai ir yra
realaus laiko „skaitmeninis dvynys", kuriuo remiasi agentų sprendimai.

**3) Įrankiais paremtas surinkimas ir galutinis suvedimas — agento vykdymo variklis (Eino).** Nestruktūrinius
ar išorinius duomenis agentas surenka **pagal poreikį** (angl. *retrieval on demand*) per įskiepiuojamus
įrankius: infrastruktūros telemetriją ir būseną — per infrastruktūros tiekėją (atvirkštiniai tuneliai →
Kubernetes / serverių API), kodo ir pakeitimų kontekstą — per GitHub įrankį, išorinį turinį — per saityno
įrankį (su SSRF apsauga), savavališkas sistemas — per Model Context Protocol (MCP) įrankius. Šie šaltiniai
galutinai sujungiami LLM „įrankių kilpoje" (planavimas → įrankio iškvietimas → rezultato stebėjimas →
tęsinys): agentas pats nusprendžia, kuriuos šaltinius užklausti konkrečiai užduočiai, ir susieja jų
rezultatus į vieną sprendimą.

**Patvarumas.** Ilgalaikiai artefaktai — pokalbių transkriptai, agento atmintis ir audito žurnalas —
patvariai saugomi **PostgreSQL** sluoksnyje, todėl surinkti duomenys išlieka prieinami tolesniems paleidimams
(atmintis / RAG).

Toks modelis — deklaratyvus, savitaisis reconcileris nutolusiai būsenai + surinkimas pagal poreikį, o ne
nuolatinis viso duomenų srauto kopijavimas — sumažina saugumo riziką (kliento duomenys nekaupiami už jų
infrastruktūros ribų daugiau, nei reikia konkrečiai užduočiai) ir yra atsparesnis trikčiams nei įvykiais
grįsti ETL sprendimai, kurie praranda nuoseklumą dingus įvykiui.

---

### 1.6. Backend kalba/framework ir frontend technologija portalui

**Backend — Go.** Visa serverinė pusė parašyta **Go** kalba (v1.26). Pasirinkta dėl našumo, statinės
tipizacijos, patikimo lygiagretumo (angl. *concurrency*) ir dėl to, kad tai yra Kubernetes bei viso debesų
(angl. *cloud-native*) ekosistemos pagrindinė kalba. Naudojami pagrindiniai Go komponentai:
- **Agentų vykdymo variklis** — pagrįstas atvirojo kodo **Eino** karkasu (LLM „įrankių kilpa", srautinis
  atsakymų generavimas, įrankių iškvietimai);
- **Valdikliai (reconcilerai)** — `controller-runtime` ir mūsų pačių vystomas **`multicluster-runtime`**;
- **Valdymo plokštuma** — atvirojo kodo **`kcp`** (Kubernetes-native, daugiaklientė izoliacija);
- **Patvarus duomenų sluoksnis** — **PostgreSQL** (transkriptai, atmintis, auditas);
- **Užklausų sluoksnis** — **GraphQL** (skaitmeninis dvynys).

**Frontend — Vue 3 + TypeScript.** Portalas kuriamas su **Vue 3** karkasu ir **TypeScript** kalba:
- **Vite** — kūrimo ir paketavimo įrankis;
- **Tailwind CSS** — stiliaus sistema;
- **Pinia** — būsenos valdymas;
- **urql** — GraphQL klientas (jungtis su skaitmeninio dvynio užklausų sluoksniu);
- **xterm.js** — interaktyvus terminalas portale;
- **Vue Router**, **markdown-it** — navigacija ir agentų atsakymų atvaizdavimas.

**Architektūrinė pastaba.** Portalas sukurtas pagal **mikro-priekinių dalių** (angl. *micro-frontend*)
principą, atitinkantį įskiepiamą tiekėjų architektūrą: kiekvienas tiekėjas gali pateikti savo UI dalį (tuo
pačiu Vue 3 / TypeScript / Vite steku), kurią pagrindinis portalas automatiškai atpažįsta ir integruoja.

---

## 2. DI komponentas ir sritys

### 2.3. Kokie LLM naudojami/planuojami + kokybės metrikos ir minimalios tikslinės reikšmės

**Naudojami / planuojami LLM.** „NAVA" pagal dizainą yra **modelių atžvilgiu neutrali** (angl.
*model-agnostic*) — tai esminė produkto vertė (Europos skaitmeninis suverenitetas): klientas pats pasirenka
modelių tiekėją. Palaikomos dvi tiekėjų šeimos:
- **OpenAI-suderinama sąsaja** — apima **OpenAI GPT-4 / GPT-4o**, **Anthropic Claude** (per suderinamą
  sąsają), **Mistral**, taip pat **savarankiškai talpinamus atvirus modelius** (**Llama, Mistral, Qwen** ir
  kt.), paleistus per vLLM / Ollama / LM Studio kliento infrastruktūroje;
- **Google** — Gemini modelių šeima.

Kiekvienas klientas pateikia savo modelių kredencialus, o agentai skirtingoms paskirtims priskiria atskirus
modelius per **profilius** (`chat` — pokalbis, `background` — foninės užduotys, `compaction` — konteksto
suspaudimas). Suvereniteto reikalaujantiems klientams visa grandinė gali veikti tik su vietiniais modeliais.

**Kokybės metrikos ir minimalios tikslinės reikšmės.** Agentų atsakymų ir veiksmų kokybę vertiname
automatizuotu vertinimo rinkiniu (angl. *evaluation harness*):

| Metrika | Ką matuoja | Min. tikslinė reikšmė |
|---|---|---|
| **Užduoties įvykdymo dažnis** (*task success rate*) | ar agentas iki galo atliko daugiapakopę užduotį | **≥ 85 %** |
| **Įrankio iškvietimo tikslumas** (*tool-call accuracy*) | ar pasirinktas teisingas įrankis su galiojančiais argumentais | **≥ 90 %** |
| **Atsakymo pagrįstumas** (*groundedness*) | ar atsakymas remiasi realiais duomenimis, o ne prasimanytas | **≥ 95 %** (halucinacijų ≤ 5 %) |
| **Patvirtinimų tikslumas** (*approval precision*) | ar rizikingi veiksmai teisingai eskaluojami žmogui | **≥ 95 %** |
| **Atsako trukmė** (*latency*, p95) | interaktyvaus atsakymo laikas | **≤ 5 s** (be įrankių vykdymo) |
| **Sąnaudos vienai užduočiai** | vidutinės LLM sąnaudos | stebima, ribojama biudžetais |

Kadangi platforma leidžia keisti modelius, vertinimo rinkinys naudojamas ir **skirtingų modelių palyginimui**.

---

### 2.6. Kaip pritaikysite modelius (fine-tuning / RAG / prompt engineering) ir kaip gimsta rezultatas

Modelių prie produkto **nepritaikome perkvalifikuodami (angl. *fine-tuning*)** — tai būtų nesuderinama su
modelių neutralumo principu ir agentiniam sprendimui nereikalinga. Taikome tris sluoksnius:

**1) Prompt engineering.** Kiekvieno agento elgseną apibrėžia persona (sisteminis kontekstas), struktūrizuotos
(JSON-schema) įrankių apibrėžtys ir konteksto lygmens patikimumo politika (neprižiūrimi paleidimai — tik
skaitymo režimu).

**2) RAG ir ilgalaikė atmintis.** Į kontekstą pagal poreikį įtraukiami realūs, kliento specifiniai duomenys:
ilgalaikė atmintis (patvariai PostgreSQL), skaitmeninis dvynys (realaus laiko būsena per GraphQL) ir įrankių
rezultatai (GitHub, saitynas, MCP, infrastruktūra).

**3) Konteksto inžinerija (compaction).** Ilgus pokalbius atskiras `compaction` modelis suspaudžia į santrauką.

**Kokius duomenis interpretuoja kiekvienas modelis (per profilius):**

| Profilis | Įvestis | Vaidmuo rezultate |
|---|---|---|
| **`chat`** | sisteminis promptas + pokalbio istorija + naudotojo žinutė + įrankių stebėjimai + atmintis/dvynys | interaktyvus planavimas ir įrankių iškvietimai |
| **`background`** | užduočių sąrašas + tvarkaraščio/įvykio duomenys + dvynio būsena | savarankiškas suplanuotų užduočių vykdymas, eskalacija žmogui |
| **`compaction`** | ilga pokalbio istorija | suspausta santrauka tolesniam kontekstui |

**Kaip gimsta galutinis rezultatas — įrankių kilpa (angl. *tool loop*).** Rezultatas nėra vienas atsakymas, o
iteracinis ciklas: modelis **suplanuoja** → **iškviečia įrankį** → gauna **stebėjimą** (realius duomenis) →
tęsia, kol užduotis įvykdyta. Rizikingi veiksmai prieš vykdymą stabdomi žmogaus patvirtinimui.

---

### 2.7. Bias, retraining dažnis, A/B testavimas

**Bias (šališkumas).** Modelio vidinis šališkumas paveldimas iš tiekėjo. Valdome: (a) **grindimu realiais
duomenimis** — agentai dirba su konkrečiais, patikrinamais duomenimis, o ne generuoja laisvą turinį;
(b) **modelio pasirinkimu** — klientas gali laisvai keisti modelį; (c) **žmogaus kontrole** — rizikingi
veiksmai reikalauja patvirtinimo.

**„Retraining" dažnis.** Tiesioginis perkvalifikavimas nevykdomas (žr. 2.6). Vietoje treniravimo turime
**prompto ir vertinimo iteracijos ciklą**: prieš kiekvieną prompto ar modelio pakeitimą paleidžiamas
vertinimo rinkinys (žr. 2.3). Naujesnės modelių versijos integruojamos ir pervertinamos tuo pačiu rinkiniu.

**A/B testavimas.** Taip, planuojamas. Profilių ir keičiamų modelių architektūra leidžia tą pačią užduotį
paleisti su skirtingais modeliais / promptais ir palyginti pagal 2.3 metrikas.

---

### 2.8. Generatyvinio DI kontrolė nuo halucinacijų (be audit log)

1. **Grindimas realiais duomenimis** — agentas veiksmus grindžia realiais įrankių stebėjimais, o ne tuo, ką
   modelis „žino"; būsenos prasimanyti negali.
2. **Struktūrizuotų argumentų validacija** — įrankių argumentai validuojami prieš vykdymą; netinkami atmetami
   ir grąžinami modeliui taisytis; dalis parametrų apriboti *enum* reikšmėmis.
3. **Konteksto lygmens patikimumo modelis** — neprižiūrimi paleidimai neturi rašymo galių (kartu — apsauga nuo
   prompt injection).
4. **Žmogaus patvirtinimo sluoksnis** — rizikingi veiksmai stabdomi patvirtinimų dėžutėje.
5. **Įvesties apsaugos** — pvz., SSRF apsauga saityno įrankyje.
6. **Sąnaudų ir kilpų ribojimas** — biudžetai apsaugo ir nuo „pabėgusių" kilpų.
7. **Audito žurnalas ir vertinimo rinkinys** — visi iškvietimai fiksuojami; pagrįstumo metrika (≥ 95 %)
   matuojama prieš kiekvieną pakeitimą.

*(Daugumą šių saugiklių sistema jau įgyvendina kode; pagrįstumo metrikos vertinimo rinkinys — Phase 2–4.)*

---

## 3. Duomenų valdymas

### 3.1. Ar apmokymo duomenys netaikomi? Jei RAG/atmintis — kokie duomenys, kiekiai, formatai

**Apmokymo duomenys — netaikomi.** Projekte **nevykdomas nuosavo modelio treniravimas ar perkvalifikavimas**.
Naudojami tik trečiųjų šalių LLM per API (arba savarankiškai talpinami atviri modeliai, taip pat per API).
Kliento duomenys nepatenka į modelio apmokymą.

**RAG ir ilgalaikė atmintis — taikoma.** Modelis dirba su kliento duomenimis tik vykdymo metu:

| Duomenų tipas | Turinys | Formatas | Kiekiai |
|---|---|---|---|
| **Pokalbių transkriptai** | žinutės, įrankių iškvietimai/rezultatai | tekstas / JSON, PostgreSQL | KB eilės vienam pokalbiui |
| **Agento atmintis** | išsaugoti faktai | trumpi teksto įrašai (iki ~8 KB) | dešimtys–šimtai vienam agentui |
| **Skaitmeninis dvynys** | serializuoti infrastruktūros objektai + ryšiai | JSON / JSONB (PostgreSQL / SQLite) | pagal kliento infrastruktūros dydį |
| **Audito žurnalas** | įrankių iškvietimų įrašai | struktūrizuotas tekstas, PostgreSQL | auga su naudojimu |
| **Konfigūracija** | persona, politika, įrankių apibrėžtys | tekstas / JSON-schema | KB eilės vienam agentui |

Kiekiai **kuklūs, teksto pobūdžio ir izoliuoti per klientą**. Į modelį patenka tik relevantiškiausi įrašai
(atrankus atgavimas + `compaction`). Duomenys saugomi kliento kontroliuojamame PostgreSQL sluoksnyje — SaaS
atveju izoliuotoje darbo erdvėje, savarankiško diegimo atveju — visiškai kliento infrastruktūroje.

---

### 3.2. GDPR (BDAR), asmens/jautrūs duomenys, EU AI Act didelės rizikos vertinimas

**Ar gali būti asmens/jautrių duomenų?** Taip, potencialiai — audito žurnaluose/telemetrijoje (naudotojų ID,
IP), pokalbių transkriptuose (bet kokie naudotojo įvesti duomenys). Todėl BDAR reikalavimus taikome **pagal
dizainą**.

**BDAR (GDPR) atitiktis.** Priklauso nuo diegimo modelio:
- **Savarankiškas diegimas** — duomenys lieka kliento infrastruktūroje; **klientas — duomenų valdytojas**,
  mūsų PĮ jų nemato (stipriausia garantija).
- **SaaS** — veikiame kaip **duomenų tvarkytojas** pagal DPA.

Priemonės (abiem atvejais): duomenų minimizavimas (surinkimas pagal poreikį); daugiaklientė izoliacija
(`kcp`); šifravimas ramybės būsenoje ir perdavimo metu, OIDC; RBAC ir auditas; duomenų subjektų teisės
(trynimas / prieiga); **jokio antrinio naudojimo** — duomenys nenaudojami modelių apmokymui.

**EU AI Act — didelės rizikos vertinimas.** Vertinome; manome, kad „NAVA" **standartiškai nepatenka į didelės
rizikos kategoriją** pagal Reglamento (ES) 2024/1689 III priedą:
- produktas — **horizontali IT operacijų automatizavimo priemonė**, o ne III priedo didelės rizikos sričių
  sistema (biometrija, įdarbinimas, švietimas, esminės paslaugos, teisėsauga, migracija, teisingumas);
- rizika mažinama **žmogaus priežiūra pagal dizainą** ir **ribotu autonomiškumu** (neprižiūrimi — tik
  skaitymo režimu);
- **GPAI tiekėjo prievolės tenka modelio tiekėjui** (OpenAI, Google...), o ne „NAVA" kaip įrankiui.

**Išlyga (konteksto priklausomybė).** Galutinį rizikos lygį lemia **kliento naudojimo kontekstas**; jei
klientas naudotų didelės rizikos srityje, tam dieginiui galėtų atsirasti papildomų prievolių. Todėl teiksime
naudojimo gaires, o žmogaus priežiūros ir audito mechanizmai suteikia klientui pagrindą įvykdyti savo prievoles.

---

### 3.3. Kodėl standartiniai metodai nepakankami? Kas naujo reikalinga?

„NAVA" — **daugelio tiekėjų (angl. *provider*) platforma** ant `kcp` karkaso, kurioje duomenys kyla iš
heterogeniškų šaltinių: **(a) nutolusi kliento infrastruktūra** (infrastructure tiekėjas — klasteriai,
serveriai per tunelius); **(b) programų kūrimo aplinkos ir „smėlio dėžės"** (app-studio tiekėjas — DI padedamo
kūrimo sandbox'ai, būsenos, kūrimo/diegimo įvykiai, kontroliniai taškai); **(c) DI agentų vykdymo duomenys**
(agents tiekėjas — paleidimai, transkriptai, atmintis, auditas). Standartiniai metodai nepakankami dėl keturių
apribojimų:

1. **Šaltinių heterogeniškumas ir paskirstymas** — reikia vieningo, tiekėjų atžvilgiu neutralaus modelio, į
   kurį naujas tiekėjas prisijungia deklaratyviai (per `APIExport` ir valdiklį), o ne atskiro ETL kiekvienam.
2. **Saugumas ir suverenitetas** — centralizuotas viso srauto kopijavimas prieštarauja produkto vertei;
   reikia surinkimo pagal poreikį per autentifikuotus tunelius.
3. **Nuoseklumas ir atsparumas** — įvykiais grįsti konvejeriai praranda nuoseklumą dingus įvykiui; reikia
   deklaratyvaus, savitaisio derinimo (reconcile) modelio (žr. 1.5), taikomo daugeliui klasterių.
4. **DI agentų samprotavimui pritaikytas modelis** — reikia realaus laiko grafinio modelio (skaitmeninio
   dvynio), kuriuo galėtų remtis tiek autonominiai agentai, tiek app-studio DI asistentas.

**Kas naujo reikalinga (taikomojo kūrimo požiūriu).** Naujumas — ne pavienis algoritmas, o **inžinerinė
sintezė** tarp kelių DI tiekėjų: (a) atvirkštinių tunelių saugus prisijungimas + (b) daugelio klasterių,
tiekėjų atžvilgiu neutralus reconcileris + (c) užklausos metu skaičiuojamas ryšių grafas (skaitmeninis
dvynys) + (d) atrankus, kontekstą suspaudžiantis įtraukimas į agentų ir DI asistento kontekstą su konteksto
lygmens patikimumo politika. Tai, kad tuo pačiu pagrindu jau veikia daugiau nei vienas DI tiekėjas (agents ir
app-studio), yra tiesioginis architektūros gyvybingumo įrodymas.

*(Tai taikomasis produkto kūrimas iš brandžių komponentų, o ne moksliniai tyrimai — žr. 4 sk.)*

---

## 4. Įgyvendinimas ir komercinimas

### 4.2. Kaip naudotojas dirbs su portalu? Pagrindiniai UX srautai

Portalas veikia kaip bendra apvalkalo (angl. *shell*) sąsaja, į kurią kiekvienas tiekėjas įsijungia savo
mikro-priekine dalimi. Naudotojas prisijungia per OIDC ir valdo savo DI agentus. Pagrindiniai srautai:

- **Agento kūrimas** — persona (sisteminis kontekstas), modelių profiliai, įrankių rinkinys, elgsenos politika.
- **Pokalbis su agentu** — natūralios kalbos langas su realaus laiko planavimu, įrankių iškvietimais ir
  rezultatais.
- **Patvirtinimų dėžutė** — rizikingi veiksmai stabdomi žmogaus patvirtinimui.
- **Autonomija** — tvarkaraščiai (cron), periodiniai patikrinimai, įvykių trigeriai.
- **Paleidimų ir ataskaitų peržiūra** — istorija, transkriptai, veiksmai, sąnaudos (kartu ir audito sąsaja).
- **Biudžetai ir nustatymai** — modelių kredencialai ir kiekvieno agento LLM sąnaudų ribos.

**Sąveikos kanalai (angl. *channels*).** Su agentu bendraujama trimis būdais:
1. **NAVA sąsaja (portalas)** — pagrindinė žiniatinklio sąsaja;
2. **MCP integracija** — agentai pasiekiami per Model Context Protocol, t. y. per išorinių modelių tiekėjų
   aplinkas / klientus (angl. *harnesses*);
3. **Kanalai kiekvienam agentui** — WhatsApp, Telegram, Discord ir el. paštas (dvipusis ryšys, įskaitant
   patvirtinimus).

**UX principas** — žmogaus kontrolė pagal dizainą: naudotojas visada mato agento veiksmus, gali įsiterpti ir
kontroliuoja sąnaudas.

---

### 4.3. Kas įeina į 12 mėn. finansuojamą projektą, o kas — ateities plėtra

**Į finansuojamą 12 mėn. projektą įeina (produkto branduolys iki TRL 8–9):**
- Agentų vykdymo variklio ir daugiaklientės izoliacijos gamybinis įtvirtinimas;
- Saugi valdymo plokštuma — atvirkštiniai tuneliai, OIDC autentifikacija;
- Patvarus duomenų saugojimas (transkriptai, atmintis, auditas);
- Autonomijos posistemis — tvarkaraščiai, periodiniai patikrinimai, įvykių trigeriai;
- Įrankių šeimos — web, GitHub, MCP, failų darbo erdvė, infrastruktūros operacijos;
- Konteksto lygmens patikimumo modelis ir patvirtinimų dėžutė;
- Sąveikos kanalai — NAVA sąsaja, MCP, WhatsApp / Telegram / Discord / el. paštas;
- Skaitmeninis dvynys (GraphQL) ir portalo vizualizacijos;
- **Savarankiškai talpinamų (angl. *self-hosted*) modelių serveriavimo integracija** (HPC/GPU);
- Biudžetai, auditas, duomenų šifravimas, saugumo kietinimas;
- **1–3 pilotiniai diegimai**, dokumentacija, diegimo paketai (Helm), komercinis paleidimas.

**Ateities plėtra (į šį projektą NEįeina):**
- Tiekėjų **rinka / katalogas** (angl. *marketplace*) ir tiekėjų sertifikavimo programa;
- Platus trečiųjų šalių tiekėjų ekosistemos augimas;
- Papildomi tiekėjai už branduolio ribų;
- Komercinės plėtros mastelio didinimas po pilotų.

**Rinkos pastaba.** Produktas pagal savo prigimtį — **atvirojo kodo ir tarptautinis nuo pirmos dienos**; tai
niekada nebus konkrečiai vienai rinkai skirtas įrankis. Atviro kodo pobūdis reiškia, kad platformą gali diegti
ir prisidėti bet kurios šalies naudotojai. Todėl **į rinką einama iš karto tarptautiniu mastu** (ES ir
platesniu), o Lietuvos / ES pilotai yra pradinis komercinis atspirties taškas, o ne apribojimas.

---

## Atviri klausimai / pastabos rengėjui

- **1.4:** 36 000 EUR = kliento sutaupymas; 24 000 EUR = pirmų metų NAVA pajamos. Suderinti su galutiniu
  kainynu, kai bus.
- **2.3 / 2.7:** vertinimo rinkinys (eval harness) ir A/B mechanizmas — **planas** (Phase 2–4), įgalintas jau
  esamos profilių architektūros. Skaičiai — tiksliniai, ne išmatuoti.
- **3.2:** pilnas šifravimas ir trynimo įrankiai — Phase 4 kietinimas; `kcp` izoliacija ir PostgreSQL jau yra.
- **Kanalai:** klausimyne — WhatsApp / Telegram / Discord / el. paštas + MCP. Pagrindiniuose grantų
  dokumentuose (§3.6, §6) minima „Slack / Telegram" — **suderinti**.
- **app-studio:** įtrauktas kaip DI tiekėjas ir duomenų šaltinis. Nuspręsti, ar jis oficialiai grantų apimtyje,
  ar tik ekosistemos gyvybingumo įrodymas.
