# Parallax — Guide utilisateur

*Français · [English](guide-utilisateur.en.md)*

Ce guide explique ce que fait Parallax et comment s'en servir, écran par
écran. Pour l'installation et la mise en service, voir
[`deploiement.md`](deploiement.md) ; pour le détail des formats de fichier,
[`import-format.md`](import-format.md).

## Sommaire

1. [Les concepts en bref](#1-les-concepts-en-bref)
2. [Se connecter, les rôles](#2-se-connecter-les-rôles)
3. [Référentiels](#3-référentiels)
4. [Catalogue matériel](#4-catalogue-matériel)
5. [Clusters et serveurs](#5-clusters-et-serveurs)
6. [Importer des données](#6-importer-des-données)
7. [Le moteur de formules](#7-le-moteur-de-formules)
8. [Besoin, offre et écart](#8-besoin-offre-et-écart)
9. [Scénarios : simuler sans rien casser](#9-scénarios--simuler-sans-rien-casser)
10. [Dimensionnement inverse](#10-dimensionnement-inverse)
11. [Comparer deux scénarios](#11-comparer-deux-scénarios)
12. [Le constructeur de vues](#12-le-constructeur-de-vues)
13. [Demandes de matériel](#13-demandes-de-matériel)
14. [Licences logicielles](#14-licences-logicielles)
15. [Adressage IP](#15-adressage-ip)
16. [Journal des modifications](#16-journal-des-modifications)
17. [Comptes et administration](#17-comptes-et-administration)
18. [Exporter n'importe quel tableau](#18-exporter-nimporte-quel-tableau)

---

## 1. Les concepts en bref

Parallax organise l'inventaire autour de quelques notions, dont la
compréhension éclaire tout le reste :

| Terme | Ce que c'est |
|---|---|
| **Cluster** | Le périmètre auquel on affecte des serveurs : la combinaison d'un projet, d'un environnement, d'une technologie, et facultativement d'un tier et d'un usage. Exemple : `ElasticHot1`. |
| **Modèle** | Une génération de matériel correspondant à une commande annuelle (type + année). Exemple : `DENSE-2025`. |
| **Révision** | Une variante datée et **immuable** d'un modèle — un ajout de disques, un changement de carte réseau. Modifier une révision déjà utilisée par un serveur est refusé ; il faut en créer une nouvelle. |
| **Composant** | Ce qu'une révision embarque : cœurs de calcul, RAM, disques (HDD/SSD séparés), cartes réseau, GPU. C'est ce qui nourrit le calcul de capacité. |
| **Serveur** | Une machine physique, avec un statut (commandé, en service, décommissionné, ou hypothétique dans un scénario) et une affectation datée à un cluster. |
| **Scénario** | Un calque d'hypothèses posé sur le réel : ajouter, déplacer ou retirer des serveurs, changer une valeur de calcul — sans jamais toucher aux données réelles tant que le scénario n'est pas retenu. |
| **Règle** | Une formule qui calcule un besoin (en cœurs, en To, en licences…) sur un périmètre donné. |
| **Variable** | Un paramètre utilisé par les règles, dont la valeur peut être différente selon le cluster, le tier, la techno, l'environnement, le projet, ou globale par défaut. |

Principe central : **le réel est un scénario comme un autre**, celui qui
n'a pas de nom. Toute lecture — une vue, un calcul de besoin, une
comparaison — accepte de se placer dans le réel ou dans un scénario, avec
la même logique partout.

## 2. Se connecter, les rôles

L'écran de connexion demande un identifiant et un mot de passe. Trois
rôles existent :

| Rôle | Peut faire |
|---|---|
| **Lecteur** | Consulter tous les écrans, construire et exporter des vues, sans rien modifier. |
| **Éditeur** | Tout ce que fait un lecteur, plus la saisie : référentiels, catalogue, serveurs, scénarios, règles, imports. |
| **Administrateur** | Tout ce que fait un éditeur, plus la gestion des comptes, le gabarit des demandes de matériel, et la consultation du journal des modifications. |

Au tout premier démarrage, un compte administrateur est créé
automatiquement (voir [`deploiement.md`](deploiement.md)). Si un annuaire
LDAP est configuré par l'administration technique, se connecter avec son
identifiant d'entreprise crée automatiquement un compte en lecteur, qu'un
administrateur promeut ensuite au rôle voulu depuis l'écran des comptes.

## 3. Référentiels

Six listes simples forment le vocabulaire commun de l'application, chacune
avec son propre écran (menu « Référentiels ») :

- **Projets**, **Environnements** (production, préproduction…),
  **Technos** (moteur de recherche, file de messages…), **Tiers** (chaud,
  tiède, froid…), **Usages**, **Zones** de disponibilité.

Chaque ligne se crée, se modifie, et s'archive plutôt que de se
supprimer : un projet archivé disparaît des listes de sélection mais reste
lisible sur tout ce qui le référence déjà — rien n'est jamais perdu.

## 4. Catalogue matériel

Menu « Catalogue » → **Modèles**. Un modèle regroupe un type et une
année ; il porte une ou plusieurs révisions, chacune datée et composée
d'éléments matériels (cœurs, RAM, disques, réseau, GPU — le détail complet
des codes reconnus est dans
[`import-format.md`](import-format.md#import-des-modèles)).

**Corriger une révision** (écraser une erreur de saisie) et **créer une
révision** (une évolution matérielle réelle, un ajout de disques par
exemple) sont deux gestes distincts et volontairement séparés dans
l'écran : une révision déjà utilisée par un serveur ne peut pas être
modifiée sans le dire explicitement. C'est ce qui garantit qu'un serveur
consulté à une date passée montre bien les caractéristiques matérielles de
l'époque.

Chaque modèle porte aussi, par technologie, le nombre de « nœuds
applicatifs » qu'il installe — utile pour les technologies qui comptent en
nœuds plutôt qu'en capacité brute.

## 5. Clusters et serveurs

Menu « Inventaire ».

**Clusters** : un cluster se crée en choisissant un projet, un
environnement et une technologie (obligatoires), un tier et un usage
(facultatifs). Des contraintes de dimensionnement (minimum, multiple,
répartition entre zones) peuvent y être ajoutées depuis l'écran
besoin/offre du cluster — voir [section 8](#8-besoin-offre-et-écart).

**Serveurs** : la fiche d'un serveur regroupe ses attributs (nom, hôte,
numéro de série, IP, VLAN, typologie, code applicatif, numéro de demande),
son rattachement à une révision du catalogue, et son affectation à un
cluster — chacune datée, avec l'historique complet conservé à chaque
changement. La liste peut se filtrer par statut, par zone, ou par
scénario.

Statuts possibles :

| Statut | Sens |
|---|---|
| `COMMANDE` | En attente de réception. |
| `EN_SERVICE` | Installé et affecté. |
| `DECOMMISSIONNE` | Sorti du parc — l'affectation et le rattachement en cours se clôturent automatiquement, l'adresse IP est libérée. |
| `HYPOTHESE` | N'existe que dans un scénario ; devient `COMMANDE` si le scénario est retenu. |

L'écran **Serveurs sans affectation active** liste les candidats naturels
à la réutilisation, triés par fin de bail croissante.

## 6. Importer des données

Menu « Exploitation » → **Import**. Quatre imports de création, à faire
dans cet ordre la première fois (chacun référence par code ce que le
précédent a créé) :

1. **Référentiels** — un seul fichier pour les six listes de la
   [section 3](#3-référentiels), une colonne `type` distinguant chaque
   ligne.
2. **Clusters**
3. **Modèles** (catalogue matériel)
4. **Serveurs**

Un cinquième import, indépendant, charge le **catalogue des VLAN** (voir
[section 15](#15-adressage-ip)).

Chaque import fonctionne en deux temps : **Analyser** ne modifie rien et
affiche toutes les anomalies détectées ligne par ligne ; **Confirmer**
écrit tout, ou rien s'il reste la moindre erreur — jamais d'import
partiel. Le format précis de chaque fichier, avec des exemples, est
détaillé dans [`import-format.md`](import-format.md).

**Mettre à jour des serveurs existants.** Un import séparé,
« Mise à jour des serveurs », modifie des serveurs déjà présents plutôt
que d'en créer. Le fichier n'a besoin de porter que les colonnes à
changer : une colonne absente du fichier, ou une cellule laissée vide, ne
touche pas le champ correspondant ; la valeur spéciale `#VIDE` efface un
champ facultatif. Le serveur visé par chaque ligne est retrouvé par son
nom d'hôte, ou par le couple numéro de demande + référence de fiche, ou
par son nom physique. Avant d'écrire quoi que ce soit, l'écran affiche un
aperçu précis des changements, ligne par ligne. Un bouton sur la liste des
serveurs (« Exporter pour mise à jour ») produit directement un fichier
dans ce format, pratique pour un aller-retour dans un tableur : on
exporte, on modifie les colonnes voulues, on réimporte.

## 7. Le moteur de formules

Menu « Moteur de formules ».

**Métriques** : les grandeurs qu'on veut calculer (disque utile en To,
RAM en Go, nombre de licences…), chacune avec son unité.

**Variables** : les paramètres qu'utilisent les formules (débit
journalier, taux de rétention, coefficient de remplissage…). Une valeur de
variable est saisie pour une année et un scénario donnés, à la portée la
plus adaptée — un cluster précis, ou plus large (tier, techno,
environnement, projet), ou une valeur par défaut globale. Au calcul,
Parallax retient toujours la valeur la plus spécifique disponible ;
chaque changement de valeur est conservé dans un historique consultable,
pour ne jamais perdre trace d'une correction.

**Règles** : une formule (expression comme `debit_jour * retention_jours`)
qui produit un besoin sur une métrique, dans le domaine défini par un
filtre (projet, environnement, techno, tier, usage, ou cluster précis).
Deux règles actives ne peuvent jamais se recouvrir sur la même métrique et
le même périmètre — Parallax le refuse à l'enregistrement en nommant la
règle en conflit, pour ne jamais laisser d'ambiguïté silencieuse sur
quelle formule s'applique.

Une règle s'évalue de deux façons possibles : **une fois sur les
agrégats** du périmètre entier, ou **serveur par serveur puis sommée** —
utile pour des plafonds qui s'appliquent machine par machine.

## 8. Besoin, offre et écart

Depuis la fiche d'un cluster : le calcul du besoin (par les règles
actives), comparé à l'offre réellement installée, avec l'écart et la
règle qui limite la capacité mise en évidence. L'écran accepte une année
et un scénario, et affiche en plus, sous scénario, ce que celui-ci change
par rapport au réel (arrivées, départs).

C'est aussi sur cet écran que se gèrent les **contraintes de
dimensionnement** d'un cluster : un nombre minimum de serveurs, un
multiple imposé, une répartition entre zones de disponibilité — des
règles complémentaires au calcul de capacité pur, qui s'appliquent au
dimensionnement inverse (section suivante).

## 9. Scénarios : simuler sans rien casser

Menu « Simulation » → **Scénarios**. Un scénario a un nom, une description
et un statut :

| Statut | Sens |
|---|---|
| `BROUILLON` | En préparation. |
| `ACTIF` | En cours d'étude, comparable et modifiable. |
| `RETENU` | Ses hypothèses ont rejoint le réel (bouton « Promouvoir ») ; consultable, non modifiable. |
| `ABANDONNE` | Écarté sans avoir touché au réel ; peut être repris. |

À l'intérieur d'un scénario actif, trois gestes possibles sur un serveur,
depuis sa fiche : le **déplacer** vers un autre cluster, le **retirer**
d'un cluster, ou **l'ajouter** (un nouveau serveur hypothétique). Une
valeur de variable peut aussi y être surchargée. Rien de tout cela ne
modifie le réel — c'est ce qui permet d'abandonner un scénario et d'en
reprendre un autre sans aucune ressaisie.

**Promouvoir** un scénario retenu bascule ses hypothèses dans le réel : les
serveurs hypothétiques deviennent réels, les déplacements et retraits
s'appliquent, les surcharges de variables remplacent les valeurs réelles.
Au même moment, les scénarios concurrents que l'on choisit explicitement
d'abandonner sont clôturés — jamais devinés automatiquement.

Tous les écrans qui affichent un état du parc (vues, besoin/offre,
comparaison) acceptent de se placer sous un scénario, exactement comme
sous le réel.

## 10. Dimensionnement inverse

Depuis la fiche d'un cluster, sous un scénario : à partir d'un besoin
cible pour une année, Parallax compare plusieurs modèles candidats du
catalogue (dans leur dernière révision) et affiche, pour chacun, le
nombre de serveurs nécessaires, le coût d'acquisition, le coût annuel, la
capacité obtenue et le surplus par rapport au besoin.

Avant de comparer, on choisit ce qui, parmi le parc déjà installé sur ce
cluster, est **conservé** — génération par génération, pas forcément la
totalité (certains modèles peuvent être obsolètes ou à récupérer
ailleurs). La capacité conservée est déduite du besoin avant de calculer
combien de nouveaux serveurs sont nécessaires.

**Matérialiser** le choix retenu crée, dans le scénario, les serveurs
hypothétiques rattachés au modèle choisi et affectés au cluster, répartis
sur les zones de disponibilité ; les serveurs installés non conservés sont
retirés du cluster dans ce même scénario. Le résultat est immédiatement
visible dans le delta de l'écran besoin/offre.

## 11. Comparer deux scénarios

Menu « Simulation » → **Comparaison**. Deux scénarios (ou un scénario et
le réel) placés côte à côte, avec les mêmes axes et agrégats au choix que
le constructeur de vues, et un écart calculé automatiquement entre les
deux colonnes. Exportable comme n'importe quel tableau.

## 12. Le constructeur de vues

Menu « Exploitation » → **Vues**. L'équivalent des tableaux croisés
dynamiques d'un tableur :

- des **axes de regroupement** libres et ordonnés (projet, environnement,
  techno, tier, usage, cluster, zone, modèle, statut du serveur…) ;
- des **filtres** à choix multiples sur chacune de ces dimensions ;
- des **colonnes agrégées** au choix : nombre de serveurs, cœurs, RAM,
  disques (HDD/SSD séparés), réseau, GPU, nœuds, coûts, unités et coût de
  licence.

Une vue se construit à la date de son choix (aujourd'hui par défaut) et
sous le scénario de son choix. Elle s'enregistre pour la retrouver plus
tard, se partage avec les autres utilisateurs, et s'exporte en CSV ou en
Excel.

Quand une colonne de licences est demandée sans que la technologie soit un
axe de regroupement, Parallax ajoute automatiquement cet axe : des
licences de technologies différentes ne se totalisent jamais entre elles.

## 13. Demandes de matériel

Menu « Exploitation » → **Demandes**. Un gabarit de texte, défini une fois
par un administrateur (menu « Administration » → **Gabarit demandes**),
décrit comment construire le descriptif à coller dans l'outil de demande
de matériel de l'entreprise, à partir de variables comme `{ip}`, `{vlan}`,
`{cluster}`, `{modele}`, `{cpu}`, `{ram}`… Le catalogue complet des
variables disponibles est affiché sur l'écran du gabarit.

L'écran Demandes liste les serveurs (filtrables par scénario, projet,
environnement, cluster, statut, avec ou sans numéro de demande), et pour
chacun : le texte généré prêt à copier, et le numéro de fiche (la
référence du serveur au sein de la demande), modifiable directement dans
le tableau avec recalcul immédiat du texte.

Une fois le numéro de demande obtenu, un geste en masse permet de
l'affecter à tous les serveurs d'une hypothèse (ou à une sélection
filtrée) en un coup, sans repasser serveur par serveur.

## 14. Licences logicielles

Menu « Moteur de formules » → **Licences**. Un contrat par technologie et
par année (éventuellement surchargé par scénario) définit comment compter
les unités dues : par nombre de nœuds installés, par plafond de RAM par
machine, ou par plafond de RAM sur l'ensemble d'un périmètre — avec un
coût unitaire associé. Le résultat apparaît comme deux colonnes du
constructeur de vues (unités, coût), calculées sur le groupe plutôt que
ligne par ligne.

Un contrat de niveau « global » n'est pas additif par construction : le
chiffre exact est celui du parc entier concerné par le contrat ; un
sous-total par projet, par exemple, reste une répartition indicative, et
la somme des sous-totaux peut dépasser le total réel. C'est signalé
directement dans les vues qui affichent ce type de colonne.

## 15. Adressage IP

Menu « Inventaire » → **VLAN**. Un catalogue de VLAN, chacun défini par un
code, des critères d'application (projet, environnement, zone, cluster —
un critère laissé vide s'applique à tous) et une ou plusieurs plages
d'adresses.

Quand une hypothèse se concrétise (au moment de la demande de devis, pas
avant), l'écran du scénario propose d'**adresser** en un geste tous ses
serveurs hypothétiques qui n'ont pas encore d'adresse : Parallax déduit le
VLAN applicable depuis le cluster et la zone du serveur (ou demande de
choisir s'il en reste plusieurs), et propose la première adresse libre
dans ses plages. Le pool d'adresses est partagé entre tous les scénarios
en cours : deux demandes de devis parallèles ne se verront jamais
proposer la même adresse. Il reste toujours possible de forcer une
adresse particulière directement sur la fiche d'un serveur.

**Anomalies réseau** (menu « Inventaire ») : une page dédiée signale, sans
jamais rien bloquer, les incohérences détectées — une adresse portée par
plusieurs serveurs, une adresse hors des plages de son VLAN, un VLAN qui
ne correspond pas au cluster ou à la zone du serveur, une adresse sans
VLAN. Un repère apparaît aussi directement sur la fiche et la ligne des
serveurs concernés.

## 16. Journal des modifications

Menu « Administration » → **Journal** (réservé aux administrateurs). Une
ligne par modification : quelle fiche, quelle action (création,
modification, correction, suppression), par qui, à quel moment, avec
l'état avant et après. Filtrable par entité, par utilisateur ou par
période, et exportable. Chaque fiche affiche aussi son propre historique
directement en bas de page.

Le journal ne propose pas d'annuler une action automatiquement : c'est un
outil pour comprendre une erreur, pas pour revenir en arrière sans
regarder — ce qui risquerait de recréer un état incohérent. Les entrées
de plus de 800 jours sont purgées automatiquement.

## 17. Comptes et administration

Menu « Administration » → **Comptes** (réservé aux administrateurs) :
créer un compte local, changer un rôle, désactiver ou réactiver un
compte, réinitialiser un mot de passe. Un compte créé automatiquement par
une connexion LDAP réussie n'a pas de mot de passe local — inutile de
tenter de le réinitialiser depuis cet écran, la case correspondante est
masquée.

**Sauvegarde** (réservé aux administrateurs) règle une copie locale
automatique de la base : dossier de destination, heure quotidienne,
rétention en jours. Une nouvelle copie n'est écrite que si quelque chose a
changé depuis la précédente — jamais de fichier redondant un soir sans
activité — et les copies plus vieilles que la rétention disparaissent
d'elles-mêmes. Un bouton **Sauvegarder maintenant** permet de vérifier
tout de suite que le dossier choisi convient, sans attendre l'heure
programmée.

## 18. Exporter n'importe quel tableau

Chaque tableau affiché dans Parallax — pas seulement les vues — porte deux
boutons, **CSV** et **Excel**, qui exportent exactement ce qui est
affiché à l'écran, filtres compris.
