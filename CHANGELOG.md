# Journal des évolutions

*Français · [English](CHANGELOG.en.md)*

Pas de numéros de version au sens SemVer : Parallax n'est pas distribué en
paquets, seulement compilé et déployé. Les repères ci-dessous (v1, v2, v3…)
reprennent le découpage fonctionnel du projet, du plus récent au plus ancien.

## v3 — Confort et automatisations

**Vue capacité.** Un écran qui confronte besoin et capacité installée sur
tout le parc : en lignes, les clusters regroupés selon les axes usuels
(projet, environnement, techno, tier, usage, cluster) ; en colonnes,
chaque composant visé par une règle, avec deux sous-colonnes besoin / capa
— sous le réel ou un scénario, pour une année, ou en projection
pluriannuelle avec l'axe « année ». Exportable. Dans le même mouvement,
l'axe « année du modèle » (génération) rejoint le constructeur de vues et
la comparaison, et tous les tableaux à axes fusionnent verticalement les
cellules de même valeur — une vraie vue hiérarchique. Les axes se
choisissent désormais par étiquettes (clic ou glisser-déposer,
réordonnables, jusqu'à six) et les en-têtes de colonnes se trient au clic,
à l'intérieur de chaque groupe parent. Les colonnes affichées se
choisissent de la même façon, dans l'ordre voulu ; par défaut : serveurs,
cœurs, RAM, HDD, SSD, nœuds.

**Synthèse de scénario et dimensionnement en lot.** Un scénario a
désormais une page d'accueil : tous ses clusters avec, pour chacun, la
règle limitante et ses chiffres avant (réel) et après (scénario), les
arrivées et départs de serveurs et le coût des ajouts — les clusters
encore en déficit ressortent, avec le lien « Dimensionner » direct. Un
raccourci ouvre la comparaison déjà réglée (réel contre ce scénario, par
cluster, serveurs, coûts, licences). Le dimensionnement en lot enchaîne
le dimensionnement inverse sur tous les clusters d'un coup : générations
conservées choisies une fois, modèle candidat par tier ou par cluster,
aperçu ligne par ligne, puis matérialisation en une seule transaction.

**Sauvegarde locale automatique.** Une copie de la base s'écrit chaque jour
sur le disque, à l'heure réglée depuis l'écran Sauvegarde — mais seulement
si quelque chose a changé depuis la précédente, pour ne jamais accumuler de
fichiers redondants un soir sans activité. Les copies plus vieilles que la
rétention réglée sont supprimées automatiquement. Un bouton dédié permet
d'en écrire une immédiatement, pour vérifier que le dossier choisi
convient. Indépendante du dépôt périodique sur stockage S3, qui reste
disponible en complément.

**Authentification LDAP.** Connexion possible via un annuaire LDAP (bind
simple sur LDAPS, sans compte de service : c'est l'identité de la personne
qui se connecte qui sert à vérifier son mot de passe). Un compte inconnu de
Parallax mais reconnu par l'annuaire est créé automatiquement en lecteur ;
un administrateur ajuste ensuite son rôle. Les comptes locaux restent
disponibles en secours — utile si l'annuaire est indisponible.

**Mise à jour en masse des serveurs.** Un second import, à côté de celui qui
crée des serveurs : celui-ci modifie des serveurs réels existants à partir
d'un fichier — réception de matériel, décommissionnement d'un lot,
réaffectation, changement de modèle, report de numéros de demande. Le
fichier ne porte que les colonnes à modifier (une colonne absente ou une
cellule vide ne change rien) et une valeur spéciale efface un champ. Avant
toute écriture, un aperçu ligne à ligne montre précisément ce qui va
changer. Un bouton sur la liste des serveurs exporte directement dans ce
format, pour l'aller-retour avec un tableur.

**Journal des modifications.** Chaque création, modification ou suppression
est tracée : qui, quand, sur quelle fiche, avec l'état avant et après. Une
page dédiée permet de filtrer par entité, utilisateur ou période ; chaque
fiche affiche son propre historique. Volontairement, le journal ne permet
pas d'annuler une action — il sert à comprendre une erreur, pas à revenir
en arrière automatiquement, ce qui aurait pu recréer des incohérences.

**Adressage IP.** Un catalogue de VLAN, chacun avec ses plages d'adresses et
les critères qui déterminent où il s'applique (projet, environnement, zone,
cluster). Quand une hypothèse se concrétise, Parallax propose les
adresses libres pour tous les serveurs concernés en un geste ; deux
hypothèses simultanées ne se voient jamais proposer la même adresse. Les
incohérences (adresse en double, hors plage, VLAN qui ne correspond pas à
l'affectation) sont signalées sur une page dédiée, sans jamais bloquer.

**Licences logicielles.** Le nombre de licences dues pour une technologie
devient une colonne calculée du constructeur de vues, avec plusieurs modes
de calcul possibles (par nœuds installés, par plafond de mémoire par
machine ou global) définis par un contrat annuel. Le contrat peut changer
de mode d'une année sur l'autre sans toucher au parc.

**Génération des demandes de matériel.** Le texte à recopier dans l'outil
de commande de l'entreprise se génère automatiquement pour chaque serveur,
à partir d'un gabarit personnalisable (`{variable}`). Un écran dédié liste
les serveurs à commander, propose le texte prêt à copier, et permet de
reporter en un geste le numéro de demande obtenu sur tout un lot de
serveurs.

**Export universel.** Tout tableau affiché dans l'application — pas
seulement les vues du constructeur — s'exporte en CSV ou en Excel d'un
clic, avec les filtres actifs à l'écran.

**Vue du parc à une date passée.** Le constructeur de vues, les vues
enregistrées et la comparaison de scénarios peuvent se replacer à une date
antérieure : utile notamment pour connaître la consommation de licences à
un instant donné.

## v2 — Simulation

**Comparaison de scénarios.** Deux hypothèses (ou une hypothèse et le réel)
placées côte à côte, avec les agrégats de son choix, exportable.

**Dimensionnement inverse.** À partir d'un besoin cible sur un cluster et
une année, Parallax compare plusieurs modèles candidats (nombre de
serveurs nécessaires, coût d'acquisition, coût annuel, capacité obtenue) et
matérialise le choix retenu en serveurs hypothétiques dans le scénario, en
tenant compte de ce qui est déjà installé et à conserver.

**Contraintes de dimensionnement.** Des règles complémentaires au calcul de
capacité pur — un minimum de serveurs, un multiple, une répartition entre
zones de disponibilité — définies par cluster, par année et par scénario.

**Scénarios par calques.** Le cœur de la simulation : une hypothèse peut
déplacer, ajouter ou retirer des serveurs, et surcharger des paramètres de
calcul, sans jamais modifier les données réelles. Elle se compare, se
promeut dans le réel ou s'abandonne à tout moment, sans ressaisie. Tous les
écrans (vues, besoin/offre) savent se placer sous un scénario.

## v1 — Référentiel fiable et consultable

**Constructeur de vues.** L'équivalent des tableaux croisés dynamiques :
axes de regroupement libres, filtres multiples, colonnes agrégées au choix,
vues enregistrées et partagées, export Excel.

**Moteur de formules.** Des règles de calcul de capacité, écrites comme de
courtes expressions et paramétrées par des variables résolues selon une
hiérarchie (cluster, tier, techno, environnement, projet, global). Deux
niveaux d'évaluation possibles selon la formule : sur l'ensemble d'un
périmètre, ou serveur par serveur puis agrégée. Un écran par cluster
compare le besoin calculé à l'offre installée et identifie la règle qui
limite la capacité.

**Import initial.** Reprise en masse des modèles et des serveurs existants
depuis des fichiers CSV, avec un mode de simulation qui liste toutes les
anomalies avant toute écriture — jamais d'import partiel.

**Clusters et inventaire.** Les clusters (regroupés par projet,
environnement, techno, tier et usage), les serveurs et leurs affectations
datées à un cluster, avec un historique complet conservé à chaque
réaffectation. L'état du parc est consultable à n'importe quelle date
passée, avec les caractéristiques matérielles de l'époque.

**Catalogue matériel.** Les modèles de serveurs, leurs révisions
(immuables — toute évolution matérielle réelle crée une nouvelle révision,
distincte d'une simple correction de saisie) et leurs composants (cœurs,
RAM, disques, réseau, GPU), qui alimentent directement le moteur de
formules.

**Socle.** Comptes locaux avec rôles (lecteur, éditeur, administrateur),
sauvegarde périodique vers un stockage compatible S3, binaire autonome sans
service externe à installer.
