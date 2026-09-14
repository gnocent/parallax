# Parallax

*Français · [English](README.en.md)*

Capacity planning et gestion des hypothèses de dimensionnement — CPU, RAM,
disque, licences — pour une équipe qui exploite des plateformes de données
de grande envergure, on-premises. L'inventaire matériel n'est pas le point
de départ mais ce qui rend ces calculs vérifiables : confronter un besoin
calculé au parc réellement installé, sans jamais risquer les données
réelles pendant qu'on teste une hypothèse.

![Démonstration de Parallax : synthèse d'un scénario, besoin et offre par cluster, dimensionnement en lot, vue capacité](docs/demoparallax.gif)

*Un tour de l'outil en quelques écrans — les données sont fictives.*

## Pourquoi

Dans une grande entreprise, ce dimensionnement se fait presque toujours
dans un tableur complexe, souvent maintenu par une seule personne : des
formules cassées par une édition concurrente, aucun historique fiable de
ce qui a été supposé et pourquoi, et surtout aucun moyen de tester une
hypothèse d'achat sans risquer d'abîmer les chiffres réels. Parallax
reprend ce même usage — des règles de calcul de besoin confrontées au
matériel disponible — mais avec une base partagée sans conflit, un
historique complet, et un principe simple : **toute hypothèse est un
calque posé sur le réel**, jamais une modification directe. On peut
construire un scénario, le comparer, l'abandonner, en reprendre un autre,
sans aucune ressaisie et sans jamais craindre d'avoir perdu la version
d'hier. L'inventaire daté du parc et le catalogue matériel en découlent :
ce sont les données de référence dont ces calculs ont besoin pour être
vérifiables, pas une fin en soi.

## Fonctionnalités

- **Moteur de formules** — règles de calcul de capacité, variables
  résolues par une hiérarchie de portées, évaluation par périmètre ou
  serveur par serveur.
- **Scénarios par calques** — simuler sans jamais toucher au réel,
  comparer deux hypothèses côte à côte, promouvoir ou abandonner d'un
  geste.
- **Dimensionnement inverse** — comparer plusieurs modèles candidats face
  à un besoin cible, en tenant compte du parc déjà installé à conserver.
- **Licences logicielles** — suivi des unités dues selon différents modes
  de calcul, comme une colonne de plus dans les vues.
- **Demandes de matériel** — génération automatique du descriptif à coller
  dans l'outil de commande, depuis un gabarit personnalisable.
- **Inventaire daté** — la photographie du parc réellement installé contre
  laquelle un besoin calculé se vérifie, avec l'historique complet
  conservé à chaque changement.
- **Catalogue matériel versionné** — modèles, révisions immuables,
  composants (cœurs, RAM, disques, réseau, GPU).
- **Constructeur de vues** — l'équivalent des tableaux croisés dynamiques,
  axes et filtres libres, export Excel.
- **Adressage IP** — catalogue de VLAN, proposition d'adresses libres,
  détection des incohérences sans jamais bloquer.
- **Journal des modifications** — qui a changé quoi, quand, avec l'état
  avant et après.
- **Authentification locale ou LDAP**, import et export CSV — y compris la
  mise à jour en masse de serveurs existants.

Le détail de chaque écran est dans le
**[guide utilisateur](docs/guide-utilisateur.md)**.

## Prérequis

- Aucun, pour utiliser un binaire prêt à l'emploi (page *Releases* du dépôt :
  Linux x64, Windows x64, macOS arm64)
- Go 1.26 ou plus, pour compiler soi-même
- Python 3.8 ou plus (uniquement pour rejouer la spécification exécutable,
  pas nécessaire à l'exécution)
- Aucune base de données à installer : SQLite est embarqué

## Démarrage rapide

Avec un binaire téléchargé depuis la page *Releases* :

```sh
./parallax -base ./parallax.db
```

Depuis les sources :

```sh
make check   # vérifie tout : spécification, compilation, vet, tests
make run     # lance l'application sur une base locale, dans ./data
```

Sur une base neuve, un compte administrateur est créé automatiquement au
premier démarrage — le mot de passe aléatoire s'affiche une fois dans les
logs de démarrage. Une fois connecté, l'écran **Import** propose cinq
fichiers CSV d'exemple, cohérents entre eux, pour explorer l'application
tout de suite sans préparer ses propres données.

## Déployer

```sh
make release
./bin/parallax -base /var/lib/parallax/parallax.db -adresse :8080
```

`CGO_ENABLED=0` et un pilote SQLite en Go pur : le binaire compilé sous
Linux tourne tel quel sur RHEL 8 ou 9, sans dépendance à la glibc de la
machine de compilation. `-base` et `-adresse` se pilotent aussi par
variables d'environnement (`PARALLAX_BASE`, `PARALLAX_ADRESSE`) — pratique
pour une unité systemd. Mise en service complète, sauvegarde périodique
vers un stockage S3, et authentification LDAP : voir
**[`docs/deploiement.md`](docs/deploiement.md)**.

## Sauvegarder

```sh
./bin/parallax -base /var/lib/parallax/parallax.db \
  -sauvegarde /tmp/parallax-$(date +%F).db
```

`VACUUM INTO` produit une copie cohérente sans interrompre les lecteurs en
cours. Le fichier obtenu est directement déposable sur un stockage
compatible S3 ; un dépôt automatique périodique est aussi disponible (voir
le guide de déploiement).

## Organisation du dépôt

```
cmd/parallax/             point d'entrée
internal/capacity/        moteur de capacité — sans dépendance externe
internal/db/               ouverture, migrations embarquées, sauvegarde
internal/depot/            couche d'accès aux données, un dépôt par entité
internal/auth/             authentification locale (argon2id) et sessions
internal/ldap/              client LDAP maison (bind simple sur TLS)
internal/sauvegarde/       client S3 maison (dépôt de sauvegarde)
internal/web/               serveur HTTP, gabarits HTML, écrans
internal/vues/              constructeur de vues — regroupement, filtres, agrégats
internal/xlsx/               écrivain .xlsx minimal, sans dépendance
internal/importation/       reprise et mise à jour en masse par CSV
spec/                        spécification exécutable et vecteurs de test
docs/                        guides utilisateur, déploiement, formats, référence technique
```

## Documentation

| Document | Pour qui | English |
|---|---|---|
| [Guide utilisateur](docs/guide-utilisateur.md) | Toute personne qui utilise l'application | [EN](docs/guide-utilisateur.en.md) |
| [Déploiement](docs/deploiement.md) | Mise en service, sauvegarde, LDAP | [EN](docs/deploiement.en.md) |
| [Formats d'import](docs/import-format.md) | Préparer des fichiers CSV | [EN](docs/import-format.en.md) |
| [Journal des évolutions](CHANGELOG.md) | Ce que chaque étape du projet a apporté | [EN](CHANGELOG.en.md) |
| [Contribuer](CONTRIBUTING.md) | Développer sur le projet | [EN](CONTRIBUTING.en.md) |
| [Modèle de données](docs/modele-donnees.md) | Référence technique du schéma, pour les contributeurs | [EN](docs/modele-donnees.en.md) |

## La spécification exécutable

`spec/capacity_reference.py` est l'implémentation de référence du moteur
de capacité. Elle produit `spec/vectors.json`, que les tests Go
consomment tels quels. La référence fait foi : toute divergence est un
bug côté Go. `spec/validate_schema.py` exécute le schéma sous SQLite avec
un jeu de données représentatif et vérifie les requêtes les plus
délicates. Détails dans [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Licence

Parallax est distribué sous
**[GNU Affero General Public License v3.0](LICENSE)** (AGPLv3) : libre
d'utilisation, d'étude, de modification et de redistribution, à condition
que le code source — y compris vos modifications — reste disponible sous
les mêmes termes, y compris quand l'outil est exposé en réseau (c'est un
serveur web).

> **Note d'intention, non contraignante juridiquement** : ce projet est né
> pour un usage interne précis et est publié dans l'idée qu'il puisse
> servir de base à d'autres équipes dans une situation comparable — un
> dimensionnement capacitaire devenu difficile à tenir dans un tableur
> partagé.
> Toute donnée propre à son contexte d'origine en a été retirée avant
> publication. C'est un partage, pas un produit : je souhaiterais qu'il
> n'en soit pas fait de revente commerciale, telle quelle ou sous une
> forme dérivée.

## Avertissement

Parallax est publié **en l'état**, sans garantie d'aucune sorte (voir les
sections 15 à 17 de l'[AGPLv3](LICENSE), qui font foi). En particulier, les
chiffres produits par le moteur de capacité dépendent entièrement des
règles et des variables que vous y saisissez : vérifiez-les contre vos
propres références avant de vous y fier pour une décision d'achat.
