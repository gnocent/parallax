# Parallax — format des fichiers d'import

*Français · [English](import-format.en.md)*

Référence autonome des quatre imports CSV (`/import/referentiels`,
`/import/clusters`, `/import/modeles`, `/import/serveurs`), pour préparer
les fichiers avant même d'avoir lancé l'application. Le format est aussi
documenté, identique, directement sur chaque page une fois l'application
démarrée.

**Ordre** : référentiels, clusters, modèles, serveurs, puis VLAN — chaque
fichier référence par code ce que les précédents ont créé. Les cinq fichiers
d'exemple (`referentiels-exemple.csv`, `clusters-exemple.csv`,
`modeles-exemple.csv`, `serveurs-exemple.csv`, `vlans-exemple.csv`)
s'importent dans cet ordre sur une base vide et donnent un jeu cohérent.
Embarqués dans le binaire (`internal/web/exemples/`), ils se téléchargent
directement depuis l'écran `/import` de l'application, sans avoir besoin du
dépôt.

Règles communes : séparateur **point-virgule** (`;`), première ligne =
en-têtes, colonnes dans n'importe quel ordre, colonnes inconnues ignorées,
BOM Excel tolérée. Un import crée, il ne met jamais à jour : un code déjà
présent en base est une erreur de ligne.

Chaque import se fait en deux temps dans l'application : **Analyser** (aucune
écriture, juste un rapport ligne par ligne) puis **Confirmer** — et si une
seule ligne est invalide, rien n'est écrit du tout, jamais d'import partiel.

---

## Import des référentiels

Un seul fichier pour les six tables, la colonne `type` distingue les lignes.

**Obligatoires** : `type` (`PROJET`, `ENVIRONNEMENT`, `TECHNO`, `TIER`,
`USAGE` ou `ZONE`), `code`, `libelle`

**Facultatives** : `ordre` (entier, ordre d'affichage — lu pour
`ENVIRONNEMENT` et `TIER`, ignoré ailleurs), `site` (lu pour `ZONE`, ignoré
ailleurs). Les projets et technos créés sont actifs.

```csv
type;code;libelle;ordre;site
PROJET;LOGS;Plateforme de logs;;
ENVIRONNEMENT;PROD;Production;1;
ENVIRONNEMENT;PREPROD;Pré-production;2;
TECHNO;ELASTIC;Elasticsearch;;
TIER;HOT;Données chaudes;1;
USAGE;INGEST;Ingestion;;
ZONE;AZ1;Zone 1;;Site A
```

## Import des clusters

**Obligatoires** : `nom`, `projet_code`, `environnement_code`, `techno_code`
(codes de référentiels existants)

**Facultatives** : `tier_code`, `usage_code`, `commentaire`

Un cluster est unique par (projet, environnement, nom).

```csv
nom;projet_code;environnement_code;techno_code;tier_code;usage_code;commentaire
ElasticHot;LOGS;PROD;ELASTIC;HOT;;Indexation temps réel
ElasticCold1;LOGS;PROD;ELASTIC;COLD;;
Kafka;LOGS;PROD;KAFKA;;INGEST;
```

## Import des modèles

**Obligatoires** : `type`, `annee`, `code`

**Facultatives** : `description`, `mode_financement` (`ACHAT` ou `LEASE`),
`duree_lease_mois`, `date_debut_lease` (`YYYY-MM-DD`), `prix_fournisseur_ht`,
`cout_annuel_ht`, `duree_cout_annees`. Les nombres décimaux acceptent la
virgule ou le point.

**Composants** (tous facultatifs — voir le vocabulaire canonique,
`docs/modele-donnees.md` §4.2). Les valeurs simples sont des totaux par
serveur ; les paires nombre / capacité unitaire se renseignent ensemble, les
éléments d'une paire étant supposés identiques :

| Colonnes | Composant créé |
|---|---|
| `cpu_coeurs` | CPU, code `cpu` — nombre total de cœurs |
| `specrate` | CPU, code `specrate` — score SPECrate base |
| `phoronix` | CPU, code `phoronix` — score Phoronix Test Suite |
| `ram_go` | RAM, code `ram` — Go au total |
| `hdd_nb` + `hdd_to` | DISQUE_DATA, code `hdd` — nombre de disques × To par disque |
| `ssd_nb` + `ssd_to` | DISQUE_DATA, code `ssd` — nombre de disques × To par disque |
| `nic_nb` + `nic_gbps` | NIC, code `nic` — nombre de cartes × Gbps par carte |
| `gpu_nb` | GPU, code `gpu` — nombre de GPU |
| `gpu_nb` + `gpu_ram_go` | GPU, code `gpu_ram` — Go par GPU |
| `gpu_nb` + `gpu_tflops_fp8` | GPU, code `gpu_fp8` — TFLOPS FP8 par GPU |
| `gpu_nb` + `gpu_bp_tos` | GPU, code `gpu_bp` — bande passante mémoire par GPU, en To/s |

Les trois attributs GPU exigent `gpu_nb` ; `gpu_nb` seul est accepté (nombre
de GPU sans caractéristiques). Les disques système ne s'importent pas : ils
n'entrent dans aucun calcul.

Si au moins un composant est renseigné sur une ligne, une révision (numéro 1,
libellé « import initial ») est créée pour le modèle et porte ces composants.

Exemple :

```csv
type;annee;code;mode_financement;cpu_coeurs;specrate;ram_go;hdd_nb;hdd_to;ssd_nb;ssd_to;nic_nb;nic_gbps
DENSE;2025;DENSE-2025;LEASE;64;480;1024;24;24;2;3,84;2;25
LEGACY;2024;LEGACY-2024;ACHAT;48;310;512;;;8;3,84;2;10
```

Un fichier complet, avec un nœud GPU : à télécharger depuis `/import` dans
l'application, ou dans `internal/web/exemples/modeles-exemple.csv`.

## Import des VLAN et plages (v3.1)

**Obligatoires** : `code`. **Facultatives** : `projet_code`,
`environnement_code`, `zone_code`, `cluster_nom` (avec `projet_code` et
`environnement_code` sur la même ligne), `ip_debut`, `ip_fin`,
`commentaire`. Un critère vide vaut « tous ». Une ligne par plage ; le VLAN
est créé une fois pour son code, avec les critères de sa première ligne
(des critères différents entre lignes d'un même code sont une erreur). Une
ligne sans `ip_debut`/`ip_fin` crée le VLAN seul. Passerelle et adresses
réservées se laissent hors intervalle.

```csv
code;projet_code;environnement_code;zone_code;cluster_nom;ip_debut;ip_fin;commentaire
LOGS-PROD-DC1;LOGS;PROD;DC1;;10.10.1.10;10.10.1.250;
LOGS-PROD-DC2;LOGS;PROD;DC2;;10.10.2.10;10.10.2.250;
```

Fichier d'exemple : à télécharger depuis `/import`, ou dans
`internal/web/exemples/vlans-exemple.csv` ; à importer après les serveurs
(les anomalies d'adressage se calculent ensuite, sans bloquer).

## Mise à jour des serveurs (v3.5)

Page `/import/serveurs/maj`. Modifie des serveurs **réels existants** ;
jamais de création (l'import de création refuse les connus, celui-ci
refuse les inconnus).

**Clé de rapprochement**, dans cet ordre, la première qui désigne
exactement un serveur : `hostname` ; sinon `demande_ref` +
`demande_serveur_ref` ; sinon `physical_name`. Ambigu ou inconnu = erreur.

**Fichier adaptatif** : seules les colonnes présentes sont touchées ; une
cellule vide ne change rien ; `#VIDE` efface. Colonnes reconnues :
`physical_name`, `hostname`, `serial_number`, `zone_code`, `position_zone`,
`ip`, `vlan_code`, `version_os`, `typologie`, `code_appli`, `demande_ref`,
`demande_serveur_ref`, `commentaire`, `statut` (COMMANDE, EN_SERVICE,
DECOMMISSIONNE), `projet_code` + `environnement_code` + `cluster_nom` +
`date_affectation` (obligatoire si le cluster change), `modele_code` +
`date_rattachement` (obligatoire si le modèle change).

L'analyse montre, par ligne, « champ : avant → après » ; l'écriture, après
confirmation, est une transaction unique et chaque changement est
journalisé au nom de l'utilisateur. « Exporter pour mise à jour » sur la
liste des serveurs produit le format exact, filtres compris.

```csv
demande_ref;demande_serveur_ref;hostname;serial_number;position_zone;statut
DEM-2026-00042;SRV-201;esh04;9Q2W7E1;Baie 14 U05-U07;EN_SERVICE
```

## Import des serveurs

**Obligatoires** : `physical_name`, `statut` (`COMMANDE`, `EN_SERVICE` ou
`DECOMMISSIONNE` — `HYPOTHESE` exige un scénario, non supporté à l'import),
`date_entree` (`YYYY-MM-DD`)

**Facultatives — attributs** : `hostname`, `serial_number`,
`zone_code` (doit déjà exister dans l'application), `position_zone`,
`ip`, `version_os`, `typologie`, `code_appli`, `demande_ref`,
`demande_serveur_ref`, `commentaire`

**Facultatives — affectation** : `projet_code`, `environnement_code` et
`cluster_nom` identifient ensemble un cluster déjà créé dans l'application.
**Les trois doivent être renseignées, ou aucune** — une seule ou deux est
une erreur de ligne. `date_affectation` facultative, par défaut
`date_entree`.

**Facultatives — rattachement de révision** : `modele_code` (doit déjà
exister et porter au moins une révision — résout automatiquement vers la
révision la plus récente de ce modèle). `date_rattachement` facultative,
par défaut `date_entree`.

Exemple :

```csv
physical_name;statut;date_entree;zone_code;projet_code;environnement_code;cluster_nom;modele_code
PHY00123;EN_SERVICE;2023-03-01;DC1;LOGS;PROD;ElasticHot1;DENSE-2023
PHY00124;EN_SERVICE;2023-03-01;DC1;LOGS;PROD;ElasticHot1;DENSE-2023
PHY00200;COMMANDE;2024-11-15;;;;;
```

(la troisième ligne : un serveur en commande, sans zone ni affectation
ni rattachement — tout facultatif peut rester vide)

## Partir d'un inventaire existant

Un tableur d'inventaire typique tient une ligne par serveur avec les colonnes
matérielles répétées (CPU, RAM, disques, prix). Parallax les sépare : ce qui
décrit une génération de matériel va dans l'import des modèles (une fois par
modèle, pas par serveur), ce qui décrit un serveur va dans l'import des
serveurs, et ce qui est une dimension de calcul (projet, environnement,
techno, tier, usage) devient un cluster à créer au préalable. Les capacités
utiles précalculées (« disque à 70 % ») ne s'importent pas : elles se
recalculent par les règles. Les colonnes de suivi manuel (cible de
réutilisation, confirmation de décommissionnement) sont remplacées par
l'affectation datée et le statut du serveur.

## Après l'import

Le jalon décisif de la bascule : vérifier que les règles de
capacité déjà déclarées reproduisent les résultats de l'Excel sur au moins
trois clusters de technologies différentes. Écran `/clusters/{id}/besoin-offre`
pour chaque cluster à vérifier — besoin, offre installée, écart, règle
limitante. Un écart avec l'Excel est une règle mal transcrite (variable,
expression, filtre) à corriger dans `/regles` et `/variables`, jamais une
tolérance à accepter.
