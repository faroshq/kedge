# Projekto aprašymas

**Produkto darbinis (kodinis) pavadinimas:** „NAVA" — autonominių DI agentų procesų automatizavimo platforma
> **Pastaba dėl pavadinimo:** „NAVA" yra **kodinis (darbinis) pavadinimas**. Galutinis produkto prekės ženklas bus patvirtintas atlikus SEO ir prekės ženklo analizę, todėl viešai naudojamas pavadinimas gali skirtis.
**Pareiškėjas:** [ĮMONĖS PAVADINIMAS], [kodas], [registracijos data]
**Kvietimas:** Finansinės paskatos startuoliams ir atžalinėms įmonėms kurti DI, blokų grandinės technologijų, robotikos, procesų automatizavimo produktus ir sprendimus
**Prašoma suma / intensyvumas:** [pvz. 100 000] EUR, iki 90 % tinkamų išlaidų; įmonės įnašas — [pvz. 20] %
**Trukmė:** 12 mėn.

---

## 1. Projekto santrauka

Projekto tikslas — sukurti ir pateikti rinkai **„NAVA" — atvirojo kodo (angl. *open source*), daugiaklientę (angl. *multi-tenant*) dirbtinio intelekto agentų platformą, kuri automatizuoja įmonių operacijų ir kasdienius darbo procesus per natūralios kalbos sąsają**. „NAVA" — **pirmiausia atvirojo kodo projektas**, kuriamas siekiant **Europos skaitmeninio suvereniteto**: europinės organizacijos gauna savarankiškai diegiamą (angl. *self-hosted*) DI agentų sprendimą, kuris nepriklauso nuo uždarų, ne ES jurisdikcijoje veikiančių platformų, leidžia laisvai rinktis modelių tiekėją (įskaitant europinius ar vietinius atvirus modelius) ir išlaikyti duomenų bei sprendimų kontrolę savo pačių infrastruktūroje.

Agentai — valdomi didelių kalbos modelių (LLM) — supranta užduotis natūralia kalba, savarankiškai planuoja veiksmus, naudoja įrankius, palaiko ilgalaikę atmintį, vykdo suplanuotas bei įvykiais paremtas užduotis ir proaktyviai informuoja žmogų per jo įprastus kanalus (Slack, Telegram, el. paštą, portalą). Vietoje pasikartojančių, rankiniu būdu atliekamų operacijų — DI agentas jas atlieka pats, su aiškiomis žmogaus patvirtinimo ribomis.

Produkto **branduolys — autonominių DI agentų platforma** (natūralios kalbos apdorojimas + robotizuotas procesų automatizavimas su žmogaus kontrole). Platforma sukurta pagal **įskiepiamą „tiekėjų" (angl. *provider*) architektūrą**: agentų galimybės plečiamos nepriklausomais moduliais, todėl DI branduolys veikia savarankiškai, o integracijos pridedamos pagal poreikį. Vienas iš tokių **pasirenkamų** tiekėjų — saugus prisijungimas prie paskirstytos kliento infrastruktūros per išeinančius atvirkštinius tunelius (be VPN, be atidarytų prievadų), leidžiantis agentams veikti bet kurioje aplinkoje su pilnu auditu. Ilgalaikėje vizijoje visi infrastruktūros ryšių komponentai („edges") tampa **pasirenkamais tiekėjais** — DI agentų platforma naudinga ir be jokios prijungtos infrastruktūros.

Produktas prieinamas dviem būdais: kaip **SaaS** (valdoma debesų paslauga) ir kaip **savarankiškai diegiamas atvirojo kodo sprendimas** kliento infrastruktūroje.

Rezultatas — komercinei rinkai parengta (TRL 8–9) SaaS ir savarankiškai diegiama platforma, mažinanti operacinių procesų sąnaudas ir žmogiškųjų klaidų riziką, orientuota į DevOps/platformų komandas, valdomų paslaugų teikėjus (MSP) ir vidutines IT organizacijas Lietuvoje bei ES.

---

## 2. Problema ir rinkos poreikis

Šiuolaikinės organizacijos valdo vis labiau **paskirstytą ir fragmentuotą infrastruktūrą**: keli Kubernetes klasteriai, debesų paskyros, pakraščio (angl. *edge*) įrenginiai, vietiniai serveriai. Kasdienių operacijų — diegimų, incidentų tyrimo, konfigūracijų atnaujinimų, atitikties patikrų, ataskaitų — didžioji dalis vis dar atliekama **rankiniu, pasikartojančiu ir klaidoms imliu būdu**, o kvalifikuotų DevOps/SRE specialistų trūksta ir jie brangūs.

Egzistuojantys automatizavimo sprendimai turi esminių spragų:

- **Scenarijais paremta automatizacija** (skriptai, CI/CD, „runbook"-ai) yra trapi — reikalauja iš anksto numatyti kiekvieną atvejį ir nuolat prižiūrima.
- **Tradicinis RPA** orientuotas į vartotojo sąsajų imitavimą, o ne į infrastruktūros ir API lygmens operacijas.
- **DI pokalbių asistentai** neturi saugaus, audituojamo veikimo prieigos prie realios kliento infrastruktūros ir nedirba autonomiškai pagal tvarkaraštį ar įvykius.
- **Prieigos prie paskirstytos infrastruktūros** organizavimas (VPN, atidaryti prievadai, kubeconfig platinimas) yra saugumo ir eksploatacijos našta.

**Esminė rinkos spraga — tiekėjo priklausomybė (angl. *vendor lock-in*) ir uždarumas.** Beveik visi šiandienos DI agentų ir asistentų produktai yra **uždari, tik debesyje veikiantys (SaaS-only) sprendimai be savarankiškai diegiamos versijos**. Klientas priverstas siųsti savo duomenis ir verslo logiką į išorinę, dažniausiai ne ES jurisdikcijoje veikiančią platformą, negali jos audituoti, yra pririštas prie vieno modelių tiekėjo ir prie vieno pardavėjo kainodaros bei tolesnio egzistavimo. Organizacijoms, kurioms svarbus **duomenų saugumas, nepriklausomumas, reguliacinė atitiktis ar suverenitetas** (viešasis sektorius, finansai, sveikatos apsauga, gynyba, kritinė infrastruktūra), tokie produktai apskritai netinka.

**Rinkoje aiškiai trūksta atviros, „AI-native" platformos DI darbo krūviams**, kurią bet kuri organizacija galėtų saugiai, nepriklausomai ir tiekėjo požiūriu neutraliai (angl. *vendor-agnostic*) paleisti **savo pačios duomenų centre ar debesyje**, laisvai pasirinkdama modelių tiekėją. Būtent šią spragą užpildo „NAVA": ji sujungia LLM pagrįstą natūralios kalbos supratimą su saugiu, audituojamu, savarankiškai diegiamu ir tiekėjui neutraliu autonominių agentų vykdymu — su aiškiomis žmogaus patvirtinimo ribomis.

---

## 3. Sprendimas — produktas „NAVA"

„NAVA" yra platforma, kurioje kiekvienas klientas (nuomininkas) izoliuotoje darbo erdvėje kuria ir valdo **savo DI agentus**. Pagrindiniai produkto komponentai:

**3.1. Autonominiai DI agentai (NKA branduolys).** Kiekvienas agentas turi personą (sisteminį kontekstą), atmintį, įrankių rinkinį ir elgsenos politiką. Vartotojas bendrauja su agentu natūralia kalba; agentas per LLM „įrankių kilpą" (planavimas → įrankio iškvietimas → rezultato stebėjimas → tęsinys) savarankiškai atlieka daugiapakopes užduotis.

**3.2. Autonominis vykdymas — tvarkaraščiai, „širdies plakimai" ir įvykių trigeriai.** Agentai veikia ne tik atsakydami į žinutes:
- **Tvarkaraščiai** (laiko juostą palaikantis cron) — pasikartojančios užduotys („kas rytą 8 val. patikrink...").
- **Periodiniai „širdies plakimai"** — agentas savarankiškai peržiūri savo užduočių sąrašą ir eskaluoja žmogui tik tada, kai reikia veiksmo (tyliai, jei nieko nauja).
- **Įvykių trigeriai** — išoriniai webhook / GitHub / kanalų įvykiai paleidžia agentą su įvykio duomenimis.

**3.3. Įrankių ekosistema ir integracijos.** Agentai naudoja įskiepiuojamus įrankius: saityno paieška ir turinio nuskaitymas (su apsauga nuo SSRF), GitHub, savavališki išoriniai įrankiai per Model Context Protocol (MCP), failų darbo erdvė, ir — svarbiausia — **prieiga prie kliento infrastruktūros** (Kubernetes klasteriai, serveriai) per platformos valdymo plokštumą.

**3.4. Saugus prisijungimas prie kliento infrastruktūros (pasirenkamas tiekėjas).** Kai agentui reikia veikti kliento aplinkoje, pasirenkamas infrastruktūros tiekėjas leidžia tai daryti saugiai: kiekvienoje aplinkoje veikiantis komponentas **inicijuoja išeinantį atvirkštinį tunelį**, todėl sistemos už NAT/ugniasienės tampa pasiekiamos per vieną autentifikuotą tašką — nereikia atidaryti prievadų ar naudoti VPN. Tai **pasirenkama** galimybė: DI agentų platforma pilnai veikia ir be jokios prijungtos infrastruktūros. Daugiaklientė izoliacija užtikrinama darbo erdvių lygmeniu (pagrįsta atviro kodo `kcp` valdymo plokštuma).

**3.5. Žmogaus kontrolė ir sauga „pagal dizainą".** Kiekvieno agento galios priklauso nuo konteksto: interaktyviame pokalbyje rizikingi veiksmai leidžiami tik gavus patvirtinimą, o neprižiūrimi (suplanuoti) paleidimai pagal nutylėjimą yra tik skaitymo režimo. **Patvirtinimų dėžutė** (portale ir kanaluose) leidžia žmogui patvirtinti ar atmesti kiekvieną rizikingą veiksmą. Visi įrankių iškvietimai fiksuojami audito žurnale. **Biudžetai** riboja kiekvieno agento mėnesines LLM sąnaudas.

**3.6. Kanalai ir portalas.** Vartotojas bendrauja su agentu ne tik per žiniatinklio portalą, bet ir iš savo įprastų pokalbių kanalų (Telegram, Slack) — dvipusiu ryšiu, įskaitant patvirtinimus.

**3.7. Operacinis „skaitmeninis dvynys".** Platforma per grafų užklausų sluoksnį (GraphQL) modeliuoja kliento paskirstytos infrastruktūros ryšius realiu laiku — sukurdama **skaitmeninį jos dvynį**, kuriuo remiasi agentų sprendimai (pvz., „kurie darbo krūviai priklauso nuo šio komponento").

**3.8. Atvira ekosistema — tiekėjų modelis ant `kcp` karkaso.** Platforma remiasi atvirojo kodo **`kcp`** (Kubernetes-native valdymo plokštumų) karkasu, todėl yra ne uždaras produktas, o **plėtiniams atvira platforma-karkasas**. Bet kuri trečioji šalis — vendorius, sistemų integratorius ar pati klientų organizacija — gali sukurti **savą tiekėją** (nuosavą API per `APIExport`, valdiklius, UI mikro-priekinę dalį, įrankių šeimas), įdiegti jį per Helm, o platforma jį automatiškai atpažįsta ir integruoja į portalą. Taip agentų ir platformos galimybės auga **už mūsų įtakos ribų**: ekosistemą plečia daugelis nepriklausomų dalyvių, o ne vienas vendorius. Tas pats `kcp` karkasas jau naudojamas kitų projektų (pvz., **platform-mesh.io**) ir vis daugiau vendorių, todėl formuojasi bendra, sąveiki (angl. *interoperable*) tiekėjų bazė ir pagrindas būsimam tiekėjų katalogui / rinkai. Standartizuota tiekėjų sąsaja reiškia, kad net infrastruktūros ryšiai ar būsimi DI moduliai gali būti kuriami trečiųjų šalių ir dalijamasi jais bendruomenėje.

---

## 4. Inovatyvumas ir produkto naujumas

„NAVA" naujumas — ne pavienė funkcija, o **unikali architektūrinė sintezė**, kurios šiuo metu nesiūlo nė vienas konkurentas:

1. **Autonominiai LLM agentai + saugi prieiga prie realios paskirstytos infrastruktūros.** Rinkos DI asistentai arba neturi veikimo prieigos prie kliento sistemų, arba ją organizuoja nesaugiai. „NAVA" agentai veikia per audituojamą, daugiaklientę valdymo plokštumą su atvirkštiniais tuneliais — tai leidžia automatizuoti realias operacijas net už ugniasienės, išlaikant įmonės saugumo reikalavimus.

2. **Konteksto lygmens patikimumo modelis (angl. *trigger-scoped trust*).** Agento leidžiami veiksmai skiriasi pagal tai, kas jį paleido ir ar žmogus stebi. Tai originalus sprendimas pagrindinei autonominių DI agentų rizikai — netiesioginėms komandų injekcijoms (angl. *prompt injection*): neprižiūrimi paleidimai neturi rašymo galių pagal nutylėjimą.

3. **Persistuojantis autonomiškumas.** Skirtingai nuo „vienkartinių" pokalbių botų, „NAVA" agentai turi ilgalaikę atmintį, veikia pagal savo tvarkaraštį, „širdies plakimus" ir įvykius, patys planuoja tolesnius veiksmus ir proaktyviai informuoja vartotoją.

4. **Įskiepiama „tiekėjų" (angl. *provider*) architektūra ant atviro `kcp` karkaso.** Platforma išplečiama nepriklausomais moduliais, todėl agentų galimybės (naujos integracijos, įrankių šeimos, net infrastruktūros ryšiai) plečiamos be pagrindinės sistemos perkūrimo. Kadangi remiamasi atvirojo kodo `kcp` karkasu, kurį naudoja ir kiti vendoriai (pvz., platform-mesh.io), ekosistema gali augti **už mūsų įtakos ribų** — tai sudaro pagrindą sąveikiai atviro kodo tiekėjų ekosistemai ir komercinei plėtrai (5 sk.: §3.8).

5. **Atviras kodas ir Europos skaitmeninis suverenitetas.** Skirtingai nuo dominuojančių uždarų, ne ES valdomų DI agentų platformų, „NAVA" yra atvirojo kodo ir gali būti diegiama pačios organizacijos infrastruktūroje su pasirenkamu (įskaitant europinį ar vietinį) modelių tiekėju. Tai leidžia europinėms organizacijoms naudoti autonominį DI neatiduodant duomenų, sprendimų logikos ir priklausomybės valdymo tretiesiems asmenims — tiesioginis indėlis į ES technologinį suverenitetą ir skaidrumą (auditą galima atlikti pačiam).

Naujumo lygis: kuriamas produktas yra **naujas rinkoje** (ne tik įmonės mastu). Projektu **nefinansuojami moksliniai tyrimai ir eksperimentinė plėtra** — visa veikla yra taikomasis, komercinei rinkai skirto **produkto kūrimas** iš esamų technologinių komponentų iki gamybinės parengties.

---

## 5. Atitiktis DI sritims ir technologinis pagrindas

Produktas tiesiogiai atitinka **tris** kvietime nurodytas DI sritis:

| DI sritis | Kaip pasireiškia produkte |
|---|---|
| **Natūralios kalbos apdorojimas (NKA)** | Pagrindinė sąsaja ir agentų „protas" — LLM supranta užduotis, planuoja, generuoja atsakymus ir ataskaitas natūralia kalba (LT/EN). |
| **Išmanioji robotika ir automatizavimas** | Robotizuotas procesų automatizavimas: agentai autonomiškai vykdo operacijų sekas per įrankius ir infrastruktūros API su politika grįsta kontrole. |
| **Skaitmeniniai dvyniai** | Realaus laiko kliento paskirstytos infrastruktūros grafinis modelis, kuriuo remiasi agentų sprendimai. |

**Didelio našumo skaičiavimai (HPC) ir didieji duomenys.** Projekte numatoma naudoti HPC / GPU infrastruktūrą LLM modelių serveriavimui ir našumo optimizavimui (pvz., [nurodyti — nacionalinė HPC bazė / LUMI / GPU debesija]), o **didžiųjų duomenų** apdorojimo konvejeriai maitina agentų atmintį, operacinį skaitmeninį dvynį ir audito analitiką (operacinės telemetrijos ir įvykių srautai iš daugelio klientų aplinkų). *(Atitinka atrankos kriterijų „HPC / didžiųjų duomenų naudojimas".)*

**Technologinis pagrindas (taikomasis, brandūs komponentai):** LLM tiekėjų API (pagal poreikį — savas modelių serveriavimas), agentų vykdymo variklis su įrankių kilpa ir kontrolinių taškų mechanizmu, atvirkštinių tunelių tinklo sluoksnis (HTTP/1.1 + WebSocket, suderinamas su bet kokiu atvirkštiniu tarpiniu serveriu), daugiaklientė valdymo plokštuma (`kcp`), patvarus duomenų sluoksnis (PostgreSQL), GraphQL užklausų sluoksnis.

---

## 6. Projekto veiklos ir darbų planas (12 mėn.)

Visa veikla — **taikomasis produkto kūrimas** iki komercinės parengties (be mokslinių tyrimų).

**1 etapas (1–3 mėn.) — Produkto branduolys ir architektūra.**
- Agentų vykdymo variklio ir daugiaklientės izoliacijos gamybinis įtvirtinimas.
- Saugios valdymo plokštumos (atvirkštiniai tuneliai, autentifikacija OIDC) parengimas gamybai.
- Patvaraus duomenų saugojimo (transkriptai, atmintis, auditas) įgyvendinimas.

**2 etapas (3–6 mėn.) — Autonomija ir įrankiai.**
- Tvarkaraščių, „širdies plakimų" ir įvykių trigerių posistemis.
- Įrankių šeimos: saitynas, GitHub, MCP integracijos, failų darbo erdvė, infrastruktūros operacijos.
- Konteksto lygmens patikimumo modelis ir patvirtinimų dėžutė.

**3 etapas (6–9 mėn.) — HPC/duomenų sluoksnis ir skaitmeninis dvynys.**
- HPC/GPU modelių serveriavimo integracija ir našumo optimizavimas.
- Didžiųjų duomenų konvejeriai atminčiai, RAG ir operaciniam skaitmeniniam dvyniui.
- Grafų (GraphQL) modelio ir portalo vizualizacijų parengimas.

**4 etapas (9–12 mėn.) — Komercinė parengtis, sauga ir pilotai.**
- Biudžetų, audito, duomenų šifravimo ir saugumo kietinimas.
- Kanalų (Slack/Telegram) ir OAuth integracijų užbaigimas.
- Pilotiniai diegimai su [1–3] klientais, dokumentacija, diegimo paketai (Helm), komercinis paleidimas.

**Rizikų valdymas:** modelių tiekėjų nepriklausomumas (BYO / keičiami modeliai), sąnaudų kontrolė per biudžetus, saugumo rizikų valdymas per konteksto patikimumo modelį ir auditą.

---

## 7. Rezultatai ir produkto parengtis

Projekto pabaigoje bus sukurtas:

- **Komercinei rinkai parengtas produktas „NAVA"** (TRL 8–9): SaaS versija ir savarankiškai diegiamas paketas.
- Ne mažiau kaip **[1–3] pilotiniai / mokami diegimai** su realiais klientais.
- Diegimo, saugumo ir naudotojo dokumentacija; komercinės licencijavimo ir kainodaros schema.
- Pagrindas tolesnei ekosistemos ir eksporto plėtrai (įskiepiama tiekėjų architektūra).

---

## 8. Rinka, klientai ir komercializacija

**Tikslinė rinka:** DevOps/platformų komandos, valdomų paslaugų teikėjai (MSP), vidutinės IT organizacijos, technologijų įmonės su paskirstyta infrastruktūra — Lietuvoje ir ES. Platesnis kontekstas — sparčiai augančios DI agentų ir IT procesų automatizavimo (RPA/AIOps) rinkos.

**Vertės pasiūlymas:** operacinių sąnaudų mažinimas automatizuojant pasikartojančias užduotis, greitesnis incidentų sprendimas, mažesnė žmogiškųjų klaidų rizika, saugi prieiga prie paskirstytos infrastruktūros be VPN.

**Komercializacijos modelis (atviro branduolio, angl. *open core*):** produkto branduolys — atvirojo kodo, laisvai diegiamas kliento infrastruktūroje; pajamos gaunamos iš **valdomos SaaS paslaugos** (prenumerata pagal aktyvius agentus/naudojimą) ir **įmonėms skirtų (angl. *enterprise*) funkcijų bei palaikymo** savarankiškai diegiamiems klientams. Atviras kodas mažina įėjimo barjerą, kuria bendruomenę ir pasitikėjimą, o suvereniteto reikalaujantiems klientams (viešasis sektorius, reguliuojamos pramonės šakos) svarbus savarankiškas diegimas be priklausomybės nuo tiekėjo. Pardavimai — tiesioginiai ir per partnerių (MSP) kanalą.

**Ekosistemos augimo variklis (tinklo efektas):** kadangi platforma remiasi bendru atviro kodo `kcp` karkasu, kurį naudoja ir kiti vendoriai (pvz., platform-mesh.io), kuo daugiau trečiųjų šalių kuria tiekėjus, tuo vertingesnė tampa platforma visiems dalyviams. Mes galime siūlyti valdomą tiekimą (SaaS), tiekėjų sertifikavimą ir palaikymą augančiai ekosistemai — pajamų šaltinis, kuris auga sparčiau nei mūsų pačių kuriamos funkcijos.

**Įėjimo į rinką strategija:** pilotai projekto metu → mokamos prenumeratos → eksportas ES rinkoje.

---

## 9. Komanda ir kompetencijos

Esame eilę metų technologijų ir atvirojo kodo srityje dirbanti įmonė, turinti gilią paskirstytų sistemų, debesų infrastruktūros, Kubernetes ir DI diegimo kompetenciją. Visi mūsų produktai yra atvirojo kodo ir kuriami vadovaujantis atvirumo principais.

**Lyderystė atvirojo kodo bendruomenėse.** Aktyviai dalyvaujame CNCF (angl. *Cloud Native Computing Foundation*, Linux Foundation) ekosistemoje ir esame projektų, kuriuos **patys vystome (angl. *lead*)**, iniciatoriai bei prižiūrėtojai:
- `kcp.io` — daugiaklienčių Kubernetes-native valdymo plokštumų karkasas (šio projekto pagrindas);
- `kbind.dev` — API paslaugų teikimo (angl. *service binding*) sprendimas;
- `multicluster-runtime` (github.com/multicluster-runtime/multicluster-runtime) — daugelio klasterių valdiklių karkasas.

Ant šių projektų statoma dalis Europos ir daugelio didelių įmonių **nepriklausomybės nuo JAV technologijų steko**. Taip pat esame aktyvūs kitų projektų (pvz., Kubernetes) dalyviai.

**Dalyvavimas ES masto projektuose.** Aktyviai prisidedame prie plataus masto Europos iniciatyvų:
- APEIRORA — https://apeirora.eu/ (t. p. https://documentation.apeirora.eu/blog/2025-03-25-kcp-multi-tenant-control-planes);
- platform-mesh.io — https://platform-mesh.io/release-0.4/ (kuriama ant to paties `kcp` karkaso — tiesioginis būsimos tiekėjų ekosistemos patvirtinimas).

**Nuosavi komerciniai produktai (Edge/AI).** Esame Edge/AI diegimo SaaS platformų **savininkai ir kūrėjai**:
- Synpse — https://synpse.com/;
- Faros — https://faros.sh/,

kuriomis jau ne vienerius metus padedame klientams valdyti infrastruktūrą sudėtingose, paskirstytose aplinkose, o vartotojai vieno mygtuko paspaudimu gali pasileisti savarankiškai talpinamus (angl. *self-hosted*) DI modelius. Tai tiesiogiai patvirtina komandos gebėjimą pristatyti būtent tokio pobūdžio — atvirą, tiekėjui neutralų, savarankiškai diegiamą DI — produktą rinkai.

**Vieši tarptautiniai pranešimai apie naująsias technologijas:**
https://www.youtube.com/watch?v=zBs2LG-Oi4w · https://www.youtube.com/watch?v=R9YUOo0MwqY · https://www.youtube.com/watch?v=y0JgZ-hQ-Bo · https://www.youtube.com/watch?v=43X0_U3cc-Y · https://www.youtube.com/watch?v=7op_r9R0fCo

**Klientai ir ankstesni komerciniai projektai.** Ne vienerius metus kartu su tarptautiniais klientais — tarp jų **Cast AI, Upbound, Clyso** — kuriame DI ir infrastruktūros sprendimus. Pardavimo pajamas patvirtinančios sąskaitos faktūros pridedamos prie paraiškos. *(Sutartys negali būti pateiktos dėl jose esančių NDA (konfidencialumo) nuostatų.)* *(Atitinka atrankos kriterijų „Ankstesni inovaciniai projektai" — ≥1 projektas per pastaruosius 2 m. ir ≥30 000 EUR pardavimo pajamų; žr. pridedamas sąskaitas faktūras.)*

---

## 10. Atitiktis kvietimo atrankos (balų) kriterijams

| Kriterijus (svoris) | Kaip projektas atitinka |
|---|---|
| **HPC / didžiųjų duomenų naudojimas** | HPC/GPU modelių serveriavimas + didžiųjų duomenų konvejeriai atminčiai, RAG ir operaciniam skaitmeniniam dvyniui (5 sk.). |
| **Ankstesni inovaciniai projektai** | [Nurodyti ankstesnius projektus ir ≥30 000 EUR pajamas — 9 sk.]. |
| **Produkto naujumas** | Nauja rinkoje architektūrinė sintezė (autonominiai agentai + saugi paskirstyta valdymo plokštuma + konteksto patikimumo modelis) — 4 sk. |
| **Bendrasis finansavimas virš 10 %** | Įmonės įnašas — [pvz. 20–30] %, viršijantis minimalų 10 % reikalavimą. |
| **DI srities fokusas** | Atitinka tris sritis: NKA, išmanioji robotika ir automatizavimas, skaitmeniniai dvyniai (5 sk.). |

---

## 11. Poveikis ir horizontalieji principai

**Ekonominis poveikis:** didinamas Lietuvos IT sektoriaus produktyvumas ir konkurencingumas, kuriamas eksportuojamas DI produktas, aukštos kvalifikacijos darbo vietos.

**Strateginis poveikis (skaitmeninis suverenitetas):** kuriamas atvirojo kodo, europinis, tiekėjui neutralus DI agentų sprendimas mažina Europos organizacijų priklausomybę nuo uždarų, ne ES valdomų platformų ir suteikia viešajam sektoriui bei reguliuojamoms pramonės šakoms galimybę naudoti autonominį DI savo pačių infrastruktūroje su pilnu auditu ir duomenų kontrole.

**Horizontalieji principai / „reikšmingai nepakenkti" (DNSH):** produktas — programinės įrangos platforma be reikšmingo tiesioginio poveikio aplinkai; energetinį efektyvumą didina biudžetų mechanizmas ir efektyvus modelių naudojimas. Sauga, privatumas ir duomenų apsauga įgyvendinami „pagal dizainą" (izoliacija, auditas, šifravimas, žmogaus kontrolė). Lygios galimybės ir prieinamumas užtikrinami produkto sąsajoje.

---

*Dokumentas parengtas kaip projekto aprašymo juodraštis. Laukai `[…]` ir darbinis pavadinimas „NAVA" pildomi / keičiami pagal pareiškėjo duomenis prieš teikiant paraišką DMS sistemoje.*
