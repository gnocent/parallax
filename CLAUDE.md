# Parallax

Capacity planning et gestion des hypothèses de dimensionnement pour une
équipe exploitant des plateformes de données de grande envergure, en
environnement on-premises cloisonné. L'inventaire matériel n'est pas le
point de départ du projet mais ce qui rend ces calculs vérifiables.
Remplace un tableur de dimensionnement devenu ingérable à plusieurs.

Ordre de grandeur visé : ~2000 serveurs, quelques rédacteurs, une quinzaine de
lecteurs. Cible de déploiement : RHEL 8 ou 9.

Références : `docs/modele-donnees.md` (le schéma et ses invariants),
`docs/backlog.md` (le découpage v1 / v2 / v3 et les décisions de cadrage),
`docs/deploiement.md` (mise en service RHEL, systemd, sauvegarde),
`docs/import-format.md` (référence autonome des formats CSV v1.4),
`CHANGELOG.md` (ce qui est livré, lot par lot).

## Vérifier

```sh
make check
```

Enchaîne spécification exécutable, `go mod tidy`, compilation, `go vet`, tests.
C'est la seule commande à connaître, et elle doit rester verte.

Test de charge à dix ans d'usage, ignoré par défaut :
`PARALLAX_PERF=1 go test ./internal/web -run TestPerformanceDixAns -v`.
Un `!!` signale un parcours de table entière : à corriger par un index
(migration additive), jamais à ignorer.

## Stack

- **Go 1.27+** (directive `go` de `go.mod`), un seul binaire autonome
- **SQLite** en WAL via `modernc.org/sqlite`, pilote Go pur, `CGO_ENABLED=0` :
  un binaire compilé sous WSL2 tourne tel quel sur RHEL 8. Ne pas réintroduire cgo.
- **HTMX** embarqué dans le binaire (pas de CDN : réseau cloisonné), rendu
  serveur avec `html/template`, pas de framework front
- **Aucun routeur tiers** : `net/http` et le `ServeMux` à motifs de Go 1.22
- **Moteur d'expressions maison** (`internal/capacity/expr.go`) : ces formules
  produisent des chiffres de budget, leur sémantique numérique reste sous notre
  contrôle. Ne pas y substituer `expr-lang/expr` ou équivalent.

`internal/capacity` n'a et ne doit avoir **aucune dépendance externe** : il se
teste hors ligne, ce qui compte quand le proxy de modules est capricieux.

## La spécification exécutable — convention centrale

`spec/capacity_reference.py` est l'implémentation de référence du moteur de
capacité. Elle produit `spec/vectors.json`, que les tests Go consomment tels quels.

**La référence Python fait foi.** Toute divergence est un bug Go. Pour faire
évoluer une règle de calcul : modifier le Python, régénérer les vecteurs
(`python3 spec/capacity_reference.py`), puis mettre le Go en conformité. Jamais
l'inverse, et jamais en ajustant un vecteur à la main pour faire passer un test.

`spec/validate_schema.py` exécute le schéma sous SQLite avec un jeu de données
représentatif et vérifie les requêtes difficiles. Toute évolution du schéma
s'accompagne d'une assertion ici.

## Glossaire métier

| Terme | Sens |
|---|---|
| **Cluster** | Périmètre de calcul : projet × environnement × techno × tier × usage × nom. C'est à lui qu'on affecte les serveurs. Ex. `ElasticCold1` |
| **Modèle** | Génération matérielle `Type+Année`, correspondant à une commande annuelle. Ex. `DENSE-2025` |
| **Révision** | Variante datée et **immuable** d'un modèle (ajout de disques, changement de carte) |
| **Composant** | Élément matériel d'une révision : nature, quantité, capacité unitaire. Vocabulaire canonique fermé : `depot.ComposantsCanoniques` (`docs/modele-donnees.md` §4.2) ; import, vues et comparaison ne connaissent que ceux-là |
| **Affectation** | Lien daté serveur → cluster. Porte les vies successives d'un serveur |
| **Scénario** | Calque de deltas sur le réel : surcharges de variables et de contraintes, ajouts et retraits de serveurs et d'affectations |
| **Règle** | Formule produisant un besoin dans une métrique, sur le domaine défini par son filtre |
| **Variable** | Paramètre de calcul indexé par (scénario, année) et par portée hiérarchique |
| **Demande** | Demande de matériel ou de devis dans l'outil interne de l'infrastructure, supposé sans API : saisie manuelle. `demande_ref` en base |
| **Réf. serveur** | Identifiant d'un serveur au sein d'une demande. `demande_serveur_ref` en base |

## Principes de conception

- **Le réel est un scénario comme un autre.** `scenario_id IS NULL` désigne le
  réel. Toute lecture accepte un scénario et résout « surcharge du scénario, à
  défaut le réel ». Ne jamais dupliquer les données du réel dans un scénario.
  Les affectations d'un scénario surchargent entièrement, jamais de fusion
  (`docs/modele-donnees.md` §6).
- **Rien n'est jamais détruit.** Abandonner un scénario le clôt ; le promouvoir
  bascule `scenario_id` à NULL, les concurrents à abandonner étant choisis
  explicitement, jamais déduits.
- **Les révisions de modèle sont immuables.** Une évolution matérielle crée une
  révision ; une correction d'erreur écrase, avec avertissement explicite. Deux
  actions, deux libellés distincts.
- **Pas d'ambiguïté silencieuse.** Deux règles actives sur la même métrique ne
  peuvent pas matcher un même cluster : refuser à l'enregistrement en nommant la
  règle concurrente. Aucune priorité implicite.
- **Le calcul reste traçable.** Le catalogue stocke la capacité brute ; les
  coefficients (formatage, remplissage, compression, réplication) sont des
  variables des formules. Ne jamais précalculer une capacité utile en base.
- **Tout est regroupable.** Le constructeur de vues remplace les tableaux
  croisés. Ne pas écrire un écran figé quand une vue répond à la question.

## Ordre de dimensionnement

Significatif — ne pas réorganiser.

```
1.  besoin[règle]   = eval(expression, variables résolues)
1b. résiduel[règle] = max(0, besoin − offre_conservée[composant_offre])
2.  nb_brut[règle]  = résiduel / composant_offre du modèle candidat
3.  nb              = max(nb_brut)   ← la gagnante est le facteur limitant
4.  contraintes du cluster : ceil, minimum, multiple, répartition zone
```

Arrondir avant le `max` donnerait un résultat différent et faux. L'offre
conservée est la capacité des serveurs installés que l'hypothèse garde, une
sélection de l'utilisateur (vide : renouvellement complet). Les contraintes
portent sur les serveurs à ajouter.

## Moteur de formules

- Deux niveaux d'évaluation : `PERIMETRE` (une fois sur les agrégats du
  cluster) et `PAR_SERVEUR` (par serveur puis somme).
- Fonctions : `abs`, `ceil`, `floor`, `max`, `min`, `round`. `round` arrondit
  au plus loin de zéro (2,5 → 3), comme la référence Python. Syntaxe et
  variables inconnues validées **à la saisie**.
- Résolution d'une variable pour (cluster, année, scénario) : scénario avant
  réel, puis portée la plus spécifique (cluster > tier > techno > environnement
  > projet > global), puis valeur par défaut.

## Hors périmètre — ne pas le proposer

- Comptabilité, amortissement, valorisation résiduelle
- La commande comme objet distinct du modèle
- La migration comme processus : c'est une réaffectation
- Unités de baie, énergie, refroidissement
- Réseau au-delà de l'IP de production et du VLAN
- Bitemporalité, event sourcing, journal d'audit réglementaire
- Champs libres génériques : la décomposition en composants les rend inutiles,
  et ils reproduiraient la dérive qui a tué l'Excel

## Conventions de code

- Dates en `TEXT` ISO-8601 `YYYY-MM-DD`. `date_fin IS NULL` = en cours.
- Migrations SQL numérotées, **jamais modifiées après application** : on ajoute un fichier.
- Identifiants, commentaires et messages en français, comme le métier.
- Pas de dépendance réseau au démarrage : l'application démarre sans S3 ni
  annuaire joignable.
- Aucun secret en base (la base est ce qu'on sauvegarde) : les clés viennent de
  l'environnement (`docs/deploiement.md`).
- Pas de SDK : client S3 (`internal/sauvegarde`, SigV4) et client LDAP
  (`internal/ldap`, bind simple LDAPv3 en BER) écrits à la main, pour rester
  compilable hors ligne. LDAPS obligatoire, pas de compte de service, mot de
  passe vide refusé avant tout bind ; au moins un administrateur local reste le
  secours.
- Les invariants du §12 de `docs/modele-donnees.md` sont posés en base quand
  c'est possible (contraintes, index uniques), dans le code sinon.
- Réglages d'administration : jamais de valeur par défaut choisie à la place de
  l'administrateur.

## Patrons et invariants du code

- Patrons de référence : `internal/depot/projet.go` pour un dépôt,
  `internal/web/handlers_projet.go` pour un écran CRUD (doc en tête de
  `internal/web/middleware.go`).
- **Journal** : toute écriture du dépôt passe par `d.journaliser` dans sa
  transaction (helpers `creerJournalise`, `modifierJournalise`,
  `ecrireJournalise`) ; les handlers d'écriture utilisent `s.depotPour(r)` pour
  porter l'auteur ; les imports appellent `JournaliserCreation`. Une méthode
  d'écriture sans journal est une régression. Le hash d'un compte n'entre jamais
  dans le journal.
- **Imports** : une transaction unique par fichier, ouverte sur `Depot.Base()` —
  aucun import partiel. Simulation puis confirmation, fichier gardé en mémoire
  entre les deux (`internal/web/import_cache.go`).
- **Matérialisation** (`depot.Materialiser`, `MaterialiserLot`) : recalcul côté
  serveur, jamais confiance à un nombre venu du formulaire ; une seule transaction.

## État

Lots v1 à v3.7 livrés (détail dans `CHANGELOG.md`, décisions dans
`docs/backlog.md`). La v2 a été livrée avant l'usage réel de la v1, sur
décision explicite.

Reste, pour qui déploie : reprise de l'inventaire existant (v1.4), puis recette
des formules contre le tableur de référence sur au moins trois clusters de
technologies différentes (v1.5, jalon décisif). Un écart est une règle mal
transcrite, pas une tolérance à accepter.
