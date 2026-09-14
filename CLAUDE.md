# Parallax

Capacity planning et gestion des hypothèses de dimensionnement pour une
équipe exploitant des plateformes de données de grande envergure, en
environnement on-premises cloisonné. L'inventaire matériel n'est pas le
point de départ du projet mais ce qui rend ces calculs vérifiables.
Remplace un tableur de dimensionnement devenu ingérable à plusieurs.

Ordre de grandeur visé : ~2000 serveurs, quelques rédacteurs, une quinzaine de
lecteurs. Cible de déploiement : RHEL 8 ou 9.

Références : `docs/modele-donnees.md` (le schéma et ses invariants),
`docs/backlog.md` (le découpage v1 / v2 / v3),
`docs/deploiement.md` (mise en service RHEL, systemd, sauvegarde),
`docs/import-format.md` (référence autonome des formats CSV v1.4).

---

## Vérifier

```sh
make check
```

Enchaîne spécification exécutable, `go mod tidy`, compilation, `go vet`, tests.
C'est la seule commande à connaître, et elle doit rester verte.

Test de charge à dix ans d'usage (5 000 serveurs, 60 000 lignes de journal),
ignoré par défaut : `PARALLAX_PERF=1 go test ./internal/web -run TestPerformanceDixAns -v`.
Il chronomètre les pages lourdes et affiche les plans des requêtes chaudes ;
un `!!` signale un parcours de table entière — à corriger par un index
(migration additive), jamais à ignorer.

---

## Stack

- **Go 1.26+** (directive `go` de `go.mod` ; le `ServeMux` à motifs date de 1.22), un seul binaire autonome
- **SQLite** en WAL via `modernc.org/sqlite` — pilote Go pur, `CGO_ENABLED=0`.
  Un binaire compilé sous WSL2 tourne tel quel sur RHEL 8 : ne pas réintroduire cgo.
- **HTMX** embarqué dans le binaire (pas de CDN : réseau cloisonné),
  rendu serveur avec `html/template`, pas de framework front
- **Aucun routeur tiers** : `net/http` et le `ServeMux` de Go 1.22
- **Moteur d'expressions maison** dans `internal/capacity/expr.go` — pas de
  bibliothèque tierce. Ces formules produisent des chiffres de budget ; leur
  sémantique numérique reste sous notre contrôle. Ne pas y substituer
  `expr-lang/expr` ou équivalent.

`internal/capacity` n'a et ne doit avoir **aucune dépendance externe** :
il se teste hors ligne, ce qui compte quand le proxy de modules est capricieux.

---

## La spécification exécutable — convention centrale

`spec/capacity_reference.py` est l'implémentation de référence du moteur de
capacité. Elle produit `spec/vectors.json`, que les tests Go consomment tels quels.

**La référence Python fait foi.** Toute divergence est un bug Go.
Pour faire évoluer une règle de calcul : modifier le Python, régénérer les
vecteurs (`python3 spec/capacity_reference.py`), puis mettre le Go en conformité.
Jamais l'inverse, et jamais en ajustant un vecteur à la main pour faire passer un test.

`spec/validate_schema.py` exécute le schéma sous SQLite avec un jeu de données
représentatif et vérifie les requêtes difficiles. Toute évolution du schéma
s'accompagne d'une assertion ici.

Ce dispositif a déjà attrapé deux bugs réels : la surcharge de variable au niveau
cluster ignorée, et `round()` divergeant entre Python et Go sur les demis.

---

## Glossaire métier

| Terme | Sens |
|---|---|
| **Cluster** | Périmètre de calcul : projet × environnement × techno × tier × usage × nom. C'est à lui qu'on affecte les serveurs. Ex. `ElasticCold1` |
| **Modèle** | Génération matérielle `Type+Année`, correspondant à une commande annuelle. Ex. `DENSE-2025` |
| **Révision** | Variante datée et **immuable** d'un modèle (ajout de disques, changement de carte) |
| **Composant** | Élément matériel d'une révision : nature, quantité, capacité unitaire. Remplace les colonnes CPU/RAM/DISK précalculées de l'Excel. Vocabulaire canonique (`depot.ComposantsCanoniques`, `docs/modele-donnees.md` §4.2) : `cpu`, `specrate`, `phoronix`, `ram` (scalaires, quantité 1), `hdd`, `ssd`, `nic`, `gpu`, `gpu_ram`, `gpu_fp8`, `gpu_bp` (dénombrés). Import, vues et comparaison ne connaissent que ceux-là |
| **Affectation** | Lien daté serveur → cluster. Porte les vies successives d'un serveur |
| **Scénario** | Calque de deltas sur le réel : surcharges de variables et de contraintes, ajouts et retraits de serveurs et d'affectations |
| **Règle** | Formule produisant un besoin dans une métrique, sur le domaine défini par son filtre |
| **Variable** | Paramètre de calcul indexé par (scénario, année) et par portée hiérarchique |
| **Demande** | Demande de matériel ou de devis auprès de l'infrastructure, dans l'outil interne de l'entreprise — supposé sans API : saisie manuelle. `demande_ref` en base |
| **Réf. serveur** | Identifiant d'un serveur au sein d'une demande. `demande_serveur_ref` en base |

---

## Principes de conception

**Le réel est un scénario comme un autre.** `scenario_id IS NULL` désigne le réel.
Toute lecture accepte un scénario en paramètre et résout « surcharge du scénario,
à défaut le réel ». Ne jamais dupliquer les données du réel dans un scénario.

**Rien n'est jamais détruit.** L'irritant numéro un de l'Excel est que les
hypothèses sont destructives. Abandonner un scénario le clôt. Promouvoir un
scénario bascule `scenario_id` à NULL.

**Les révisions de modèle sont immuables.** C'est ce qui donne l'historique sans
bitemporalité. Une évolution matérielle crée une révision ; une correction
d'erreur écrase, avec avertissement explicite. Deux actions, deux libellés distincts.

**Pas d'ambiguïté silencieuse.** Deux règles actives sur la même métrique ne
peuvent pas matcher un même cluster. Vérifier à l'enregistrement et refuser en
nommant la règle concurrente. Aucune priorité implicite.

**Le calcul reste traçable.** Le catalogue stocke la capacité brute ; les
coefficients (formatage 0,9, remplissage max 0,7, compression, réplication) sont
des variables utilisées dans les formules. Ne jamais précalculer une capacité
utile en base.

**Tout est regroupable.** Le constructeur de vues remplace les tableaux croisés :
axes libres, filtres multiples, agrégats, export Excel. Ne pas écrire un écran
figé quand une vue répond à la question.

---

## Ordre de dimensionnement

Significatif — ne pas réorganiser.

```
1.  besoin[règle]   = eval(expression, variables résolues)
1b. résiduel[règle] = max(0, besoin − offre_conservée[composant_offre])
2.  nb_brut[règle]  = résiduel / composant_offre du modèle candidat
3.  nb              = max(nb_brut)   ← la gagnante est le facteur limitant
4.  contraintes du cluster : ceil, minimum, multiple, répartition zone
```

Arrondir avant le `max` donnerait un résultat différent et faux.

L'offre conservée (v2.3) est la capacité des serveurs déjà installés que
l'hypothèse garde — une **sélection** de l'utilisateur, par génération
typiquement (on garde les modèles récents, on sort les obsolètes ou ceux à
récupérer), pas forcément tout le parc. Vide : renouvellement complet. Les
contraintes portent sur les serveurs à ajouter.

## Moteur de formules

Deux niveaux d'évaluation, tous deux nécessaires :

- `PERIMETRE` — évaluation unique sur les agrégats du cluster
- `PAR_SERVEUR` — évaluation par serveur puis somme

Fonctions disponibles : `abs`, `ceil`, `floor`, `max`, `min`, `round`.
`round` arrondit au plus loin de zéro (2,5 → 3), sémantique alignée avec la
référence Python. Syntaxe et variables inconnues validées **à la saisie**.

Résolution d'une variable pour (cluster, année, scénario) : scénario avant réel,
puis portée la plus spécifique (cluster > tier > techno > environnement >
projet > global), puis valeur par défaut de la variable.

---

## Hors périmètre — ne pas le proposer

- Comptabilité, amortissement, valorisation résiduelle
- La commande comme objet distinct du modèle
- La migration comme processus : c'est une réaffectation
- Unités de baie, énergie, refroidissement
- Réseau au-delà de l'IP de production et du VLAN
- Bitemporalité, event sourcing, journal d'audit réglementaire
- Champs libres génériques : la décomposition en composants les rend inutiles,
  et ils reproduiraient la dérive qui a tué l'Excel

---

## Conventions de code

- Dates en `TEXT` ISO-8601 `YYYY-MM-DD`. `date_fin IS NULL` = en cours
- Migrations SQL numérotées, **jamais modifiées après application** : on ajoute
  un fichier. `0001` a été réécrite en place deux fois avant toute mise en
  service (généralisation des noms, puis vocabulaire des composants le
  2026-09-11) — plus jamais après le premier déploiement
- Identifiants, commentaires et messages en français, comme le métier
- Pas de dépendance réseau au démarrage : l'application démarre sans S3 joignable
- Aucun secret en base : la base est ce qu'on sauvegarde, et tout lecteur de la
  base lirait les clés de son propre dépôt de sauvegardes. Les clés S3 viennent
  de l'environnement (fichier d'environnement systemd en 0600, voir
  `docs/deploiement.md`)
- Client S3 maison (`internal/sauvegarde`, SigV4 écrit à la main) : pas de SDK,
  le module doit rester compilable hors ligne
- Client LDAP maison (`internal/ldap`, bind simple LDAPv3 encodé en BER à la
  main, LDAPS obligatoire, DN par gabarit `{login}`) : même règle. Pas de
  compte de service ; l'annuaire n'est jamais contacté au démarrage ; un
  mot de passe vide est refusé avant tout bind (un bind anonyme réussirait) ;
  au moins un administrateur local reste le secours
- Les invariants du §12 de `docs/modele-donnees.md` sont posés en base quand
  c'est possible (contraintes, index uniques) et dans le code sinon

---

## État

| Lot | État |
|---|---|
| Schéma et migrations | validés par exécution réelle (`spec/validate_schema.py`) ; migration `0002` : invariants 3 et 4 en index partiels |
| Moteur de capacité | écrit, couvert par 45 vecteurs |
| Socle : ouverture base, migrations, sauvegarde, binaire | écrit |
| Couche d'accès aux données (dépôts par entité) | écrite, `internal/depot`, ~95 tests sur base temporaire migrée |
| Couche web : tous les écrans v1.1-v1.6, moteur de formules, import | écrite, `internal/web` + `internal/auth` + `internal/vues` + `internal/xlsx` + `internal/importation` |
| v2.1 — Scénarios par calques | écran complet (voir plus bas), en avance sur le séquencement suggéré du backlog |
| v2.2 — Contraintes de dimensionnement | section de la page besoin/offre (`handlers_contrainte.go`), par année et scénario |
| v2.3 — Dimensionnement inverse multi-modèles | `handlers_dimensionnement_inverse.go`, offre conservée (étape 1b), matérialisation transactionnelle (`depot.Materialiser`) |
| v2.4 — Comparaison de scénarios | `handlers_comparaison.go`, agrégats au choix, les deux prix, exports ; préréglable par l'URL (axes, colonnes → résultat rendu d'emblée) |
| v2.5 — Synthèse de scénario et lot | `handlers_scenario_synthese.go` : `/scenarios/{id}/synthese` (tous clusters, avant / après sur la règle limitante, mouvements, coût des ajouts, lien comparaison préréglé), `/scenarios/{id}/lot` (générations conservées globales, modèle par tier puis par cluster, aperçu, `depot.MaterialiserLot` en une transaction) ; `contexteBesoin` charge règles et variables une fois pour tous les clusters |
| v2.6 — Vue capacité | `handlers_capacite.go`, `/capacite` : clusters regroupés par axes (projet, environnement, techno, tier, usage, cluster), une colonne par composant visé par une règle active, sous-colonnes besoin / capa ; besoin par (cluster, composant) = max des règles, puis somme par groupe ; réel ou scénario ; axe `annee` = projection pluriannuelle (plage `annee`–`annee_fin`, dix ans max, pas de Total) ; export universel. `vues.DimAnneeModele` (génération) dans vues et comparaison ; `fusionsVerticales` (`fusion.go`) : rowspan des cellules d'axes sur les trois écrans, exports plats. `maxAxes` = 6 (`selecteur_axes.go`, gabarit `selecteur_axes`, `static/axes.js` : étiquettes cliquables / glissables → champs cachés `axe1…axeN`, contrat de formulaire inchangé). `tri.go` : `tri`/`sens` dans l'URL, `ordreTri` trie par valeur dans le groupe parent (jamais à travers), en-têtes `enteteTri` — hx-get sur vues/comparaison (tri rattaché au formulaire par `form=`), liens GET sur capacité |
| Sauvegarde S3 (v1.1, item resté ouvert) | `internal/sauvegarde` (SigV4 maison, CA interne), dépôt périodique et `-sauvegarde-s3` dans `cmd/parallax` |
| v3.0 — Export universel, vue parc à une date | `tableau.go` (`s.exporter`, `exporterNomme`, `export_boutons`), test de couverture des gabarits ; `ChargerParcCourant(base, scénario, date)` |
| v3.2 — Demandes de matériel | `gabarit.go` (`{nom}`), `handlers_demande.go`, `handlers_parametre.go`, `depot/demande.go`, `depot/parametre.go` ; typologie en énumération |
| v3.3 — Licences | `depot/licence.go`, `vues/licences.go` (colonnes calculées par groupe, axe techno automatique), `handlers_licence.go` |
| v3.1 — Adressage IP | `depot/ip.go`, `vlan.go`, `adressage.go` ; `handlers_vlan.go`, `handlers_import_vlans.go`, `handlers_adressage.go` ; fiche serveur (VLAN, attribution, anomalies) |
| v3.4 — Journal | `depot/journal.go` (auteur par `Depot.Au`, `s.depotPour(r)` côté web, hooks dans toutes les écritures et les imports), `handlers_journal.go`, purge quotidienne |
| v3.5 — Mise à jour en masse des serveurs | `handlers_import_maj.go` (fichier adaptatif, `#VIDE`, diff), `depot.AppliquerMajServeurs` (une transaction), export aller-retour |
| v3.6 — Authentification LDAP | `internal/ldap` (bind simple LDAPv3 sur TLS, BER à la main), `auth.AuthenticatorLDAP` (local d'abord, annuaire sinon, création en LECTEUR), `utilisateur.origine` (migration `0005`), `PARALLAX_LDAP_*` |
| v3.7 — Sauvegarde locale automatique | `internal/sauvegarde/locale.go` (écriture horodatée, listing, purge par rétention), `depot.ActiviteDepuis` (détection sur le journal, entité `parametre` exclue), `planifierSauvegardeLocale` dans `cmd/parallax` (vérification chaque minute, un seul passage par jour), écran `handlers_sauvegarde.go` (`/parametres/sauvegarde`, dossier/heure/rétention réglés en base, jamais de défaut choisi à la place de l'administrateur) — indépendant du dépôt S3 périodique |

`internal/depot` couvre toutes les entités : référentiels, catalogue et
révisions, clusters, serveurs, rattachements et affectations datés, scénarios
(dont promotion), variables (résolution + historique), métriques, règles
(refus de recouvrement) et contraintes. Patron de référence : `projet.go`.
Les invariants du §12 y sont tenus (immuabilité des révisions, non-recouvrement
des règles, seau scénario des affectations).

`internal/auth` : comptes locaux argon2id derrière `Authenticator`, sessions
côté serveur (migration `0003`), CSRF synchronizer par session (exempté sur
GET/HEAD/OPTIONS). `internal/web` : net/http + ServeMux Go 1.22, html/template
embarqué, HTMX vendorisé (pas de CDN). Patron CRUD de référence :
`handlers_projet.go` (doc en tête de `middleware.go`). Écrans livrés : tous
les référentiels, catalogue avec l'UX d'immuabilité des révisions (créer /
corriger, deux actions distinctes), clusters, serveurs (attributs,
rattachement de révision, affectation datée, vue « sans affectation
active »), comptes (admin), métriques/variables (déclaration + valeurs par
portée + historique)/règles (refus de recouvrement, activation, duplication),
l'écran besoin / offre / écart par cluster (`internal/capacity.CalculerBesoins`,
extrait de `Dimensionner` — même besoin, sans capacité de modèle candidat),
et le constructeur de vues v1.6 (`internal/vues`, export CSV et xlsx maison
via `internal/xlsx`, vues sauvegardées et partagées). Compte administrateur
amorcé automatiquement au premier démarrage si aucun ADMIN n'existe.

`internal/importation` (v1.4) : reprise CSV des référentiels (six tables en
un fichier, colonne `type`), des clusters, des modèles et des serveurs, dans
cet ordre.
Résolution des références en lecture via `internal/depot`, écriture dans une
transaction unique par fichier ouverte directement sur `Depot.Base()` — les
méthodes d'écriture du dépôt n'exposent pas de transaction partagée à
l'appelant, nécessaire pour l'invariant « aucun import partiel ». Simulation
et confirmation en deux temps, fichier gardé en mémoire entre les deux
(`internal/web/import_cache.go`, jeton éphémère 15 min).

Le pipeline capacité (résolution de portée, règle limitante, écart) est
vérifié sur un jeu de données de simulation à trois technologies, calculé
indépendamment à la main — correspondance exacte. Ce n'est pas le jalon de
recette v1.5 lui-même (qui compare aux vraies données, confidentielles,
absentes de cet environnement) mais une preuve que le moteur fait ce qu'il
prétend faire.

Durci pour le déploiement : configuration par variables d'environnement
(`PARALLAX_BASE`, `PARALLAX_ADRESSE`), arrêt propre sur SIGTERM/SIGINT,
`/sante` sans authentification. Voir `docs/deploiement.md`.

**v2.1 — Scénarios par calques**, livrée par anticipation. Le principe posé
plus haut (« la v2 démarre après une période d'usage réel de la v1 ») a été
sciemment écarté sur décision explicite : la validation sur les vraies
données prendrait trop de temps à distance, et un écart mineur découvert
plus tard se traite à ce moment-là. La couche dépôt et le moteur de capacité
portaient déjà cette dimension depuis leur écriture initiale (`scenario_id`,
`AffectationsResolues`, `ResoudreVariable`, `ContraintesResolues` — « le réel
est un scénario comme un autre » était un principe de schéma, pas encore un
principe d'écran) ; ce lot a construit les écrans qui manquaient :

- écran de gestion des scénarios (`handlers_scenario.go`) : CRUD, cycle de
  statut (BROUILLON → ACTIF → ABANDONNE → ACTIF), et promotion en page
  dédiée avec sélection explicite des scénarios concurrents à abandonner
  (jamais déduite automatiquement) ;
- `AffecterDansScenario` et `RetirerDuScenario` (`internal/depot/affectation.go`)
  pour déplacer ou retirer un serveur — réel ou hypothétique — dans le seau
  d'un scénario, sans jamais toucher le réel ; branchés sur la section
  affectation de la page de détail d'un serveur ;
- création d'un serveur HYPOTHESE depuis l'écran des serveurs, rattaché à un
  scénario (invariant 5) ;
- surcharges de variables par scénario sur l'écran de détail d'une variable ;
- besoin / offre / écart et constructeur de vues (v1.6) paramétrables par
  scénario, avec un delta (arrivées/départs) sur l'écran besoin/offre — c'est
  l'« écran unique par cluster » visé par le point de vigilance ergonomique
  du backlog (docs/backlog.md, v2.1).

Règle de résolution ajoutée à `modele-donnees.md` §6 pour les affectations :
surcharge entièrement, jamais fusion — un serveur touché par un scénario
(déplacé, retiré ou ajouté) n'apparaît plus, en double, sous sa forme réelle.

**v2.2, v2.3, v2.4 et sauvegarde S3** (2026-09-10, décisions de cadrage dans
`docs/backlog.md`). Contraintes en section de la page besoin/offre, chargée
en htmx. Dimensionnement inverse (`/clusters/{id}/dimensionnement`) : on
coche ce qu'on conserve du parc installé par génération et les modèles
candidats (dernière révision) ; `capacity.Dimensionner` reçoit l'offre
conservée ; la matérialisation recalcule côté serveur (jamais confiance à un
nombre venu du formulaire) puis `depot.Materialiser` pose, en une
transaction, les serveurs HYPOTHESE (révision rattachée, affectés, répartis
sur les zones du cluster) et retire les non conservés. Comparaison
(`/comparaison`) : A et B (réel ou scénario), axes et colonnes du
constructeur de vues, Δ, exports. Sauvegarde : `docs/deploiement.md` §6.2.

**v3** (2026-09-12, spécifiée par entretiens — guide hors dépôt ; décisions
dans `docs/backlog.md`). Journal : toute écriture du dépôt passe par
`d.journaliser` dans sa transaction (helpers `creerJournalise`,
`modifierJournalise`, `instantane` + `journal*Table`, `ecrireJournalise`
pour les gestes composites) ; les handlers d'écriture utilisent
`s.depotPour(r)` pour porter l'auteur ; les imports appellent
`JournaliserCreation`. Le hash d'un compte n'entre jamais dans le journal.
Une nouvelle méthode d'écriture sans journal est une régression.

Suite : bascule sur le poste de travail réel — import des 1800 lignes
existantes (v1.4), recette des formules contre l'Excel sur au moins trois
clusters de technologies différentes (v1.5, jalon décisif). Puis v3
(génération des demandes de matériel, licences, IP, LDAP, import
incrémental) : chacun demande d'abord des faits que seul l'usage réel
fournit (formats, formules, annuaire).

Jalon de recette de la v1 : les formules reproduisent les résultats de l'Excel
actuel sur au moins trois clusters de technologies différentes. Un écart est une
règle mal transcrite, pas une tolérance à accepter.
