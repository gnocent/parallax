# Contribuer à Parallax

*Français · [English](CONTRIBUTING.en.md)*

## Avant de coder

Lire [`CLAUDE.md`](CLAUDE.md) : c'est la référence technique du projet —
stack, conventions, glossaire métier, ordre du calcul de capacité,
organisation du dépôt, et l'état de chaque fonctionnalité. Il a été rédigé
pour guider un assistant IA travaillant sur le code, mais il est tout aussi
utile à une personne qui découvre le projet.

Pour comprendre le schéma de données et ses invariants dans le détail,
[`docs/modele-donnees.md`](docs/modele-donnees.md) est la référence
technique. Le [guide utilisateur](docs/guide-utilisateur.md) explique ce
que fait l'application ; ce document explique comment elle le fait.

## Environnement

- Go 1.26 ou plus (la version exigée par `go.mod`)
- Python 3.8 ou plus, uniquement pour rejouer la spécification exécutable
  (absent : `make check` s'appuie alors sur les vecteurs déjà commités)
- Aucun service externe : SQLite est embarqué, aucune base à installer

## Vérifier son travail

```sh
make check
```

Rejoue la spécification exécutable, `go mod tidy`, la compilation, `go vet`
et l'ensemble des tests. C'est la seule commande à connaître, et le projet
attend qu'elle reste verte à chaque commit.

## La spécification exécutable

`spec/capacity_reference.py` est l'implémentation de référence du moteur de
capacité. Elle produit `spec/vectors.json`, que les tests Go consomment
tels quels. **La référence Python fait foi** : toute divergence est un bug
côté Go. Pour faire évoluer une règle de calcul, on modifie le Python
d'abord, on régénère les vecteurs, puis on met le Go en conformité — jamais
l'inverse, et jamais en ajustant un vecteur à la main pour faire passer un
test.

`spec/validate_schema.py` exécute le schéma sous SQLite avec un jeu de
données représentatif et vérifie les requêtes les plus délicates
(héritage des variables, état du parc à une date passée, invisibilité d'un
serveur hypothétique hors de son scénario, détection de chevauchement
entre règles).

## Conventions à respecter

- Identifiants, commentaires et messages d'erreur en français, comme le
  métier que l'outil sert.
- Dates en texte ISO-8601 (`YYYY-MM-DD`) ; `date_fin IS NULL` signifie « en
  cours ».
- Migrations SQL numérotées, jamais modifiées après leur première
  application — toute évolution du schéma ajoute un nouveau fichier.
- Aucun secret en base : la base est ce que l'on sauvegarde, et un secret
  qui s'y trouverait finirait dans chaque copie de sauvegarde. Les clés
  d'accès externes (S3, LDAP) viennent uniquement de variables
  d'environnement.
- Pas de dépendance réseau au démarrage : l'application démarre même si
  S3 ou l'annuaire LDAP configuré sont injoignables.
- Le moteur d'expressions (`internal/capacity/expr.go`) et les clients S3
  et LDAP sont écrits à la main, sans bibliothèque tierce, pour rester
  auditable et compilable hors ligne.

## Tester la charge

Un test de performance simule dix ans d'usage (plusieurs milliers de
serveurs, dizaines de milliers de lignes de journal) et signale toute
requête qui parcourrait une table entière plutôt que de passer par un
index. Il est ignoré par défaut :

```sh
PARALLAX_PERF=1 go test ./internal/web -run TestPerformanceDixAns -v
```

## Publier une release

Un tag `vX.Y.Z` poussé sur le dépôt déclenche `.github/workflows/release.yml` :
rejoue `make check`, construit les trois binaires de poste de travail
(`make release-tous` : Linux x64, Windows x64, macOS arm64) et les attache à
la release GitHub correspondante. Rien à faire à la main au-delà du tag.

## Style de contribution

Un commit correspond à un changement cohérent et testé. Les messages
expliquent le pourquoi, pas seulement le quoi — voir l'historique du
projet pour le ton attendu.
