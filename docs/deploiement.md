# Parallax — déploiement

*Français · [English](deploiement.en.md)*

Ce document couvre la mise en service sur un serveur RHEL 8 ou 9 interne.
Il complète les principes de CLAUDE.md sans les
répéter.

---

## 1. Compiler

Depuis un poste avec le toolchain Go (WSL2 ou Linux), à la racine du dépôt :

```sh
make release
```

Produit `bin/parallax`, un exécutable Linux amd64 **statiquement lié** —
`CGO_ENABLED=0` et le pilote SQLite pur Go (`modernc.org/sqlite`) le
garantissent. Vérifiable :

```sh
file bin/parallax   # « statically linked »
ldd bin/parallax     # « not a dynamic executable »
```

Aucune dépendance à la version de glibc de la machine de compilation : le
binaire tourne tel quel sur RHEL 8 ou 9, sans rien installer d'autre que le
transférer.

## 2. Transférer et premier lancement

```sh
scp bin/parallax serveur-cible:/opt/parallax/parallax
ssh serveur-cible
sudo mkdir -p /var/lib/parallax
sudo /opt/parallax/parallax -base /var/lib/parallax/parallax.db -adresse :8080
```

Au tout premier lancement sur une base neuve, un compte administrateur est
créé automatiquement et son mot de passe **imprimé une seule fois** dans les
logs :

```
aucun compte administrateur trouvé : compte créé — login « admin », mot de passe « ... »
```

Note-le immédiatement (il n'est pas ré-affichable) et change-le dès la
première connexion (écran Comptes → Mot de passe). Arrête le lancement
manuel (Ctrl+C — l'arrêt est propre, voir §5) avant de passer à l'unité
systemd.

## 3. Configuration

Deux façons équivalentes, la seconde utile pour une unité systemd :

| Flag | Variable d'environnement | Défaut | Rôle |
|---|---|---|---|
| `-base` | `PARALLAX_BASE` | `parallax.db` | chemin du fichier SQLite |
| `-adresse` | `PARALLAX_ADRESSE` | `:8080` | adresse d'écoute HTTP |

Un flag explicite sur la ligne de commande l'emporte sur la variable
d'environnement.

Pas de TLS intégré : le binaire sert du HTTP simple. Sur un réseau
cloisonné, c'est un reverse proxy (nginx, haproxy) qui termine le TLS et
transmet en HTTP interne — configuration hors du périmètre de ce document,
à la charge de l'équipe infrastructure qui gère déjà ces reverse proxies.

## 4. Unité systemd

`/etc/systemd/system/parallax.service` :

```ini
[Unit]
Description=Parallax — inventaire matériel et capacity planning
After=network.target

[Service]
Type=simple
User=parallax
Group=parallax
Environment=PARALLAX_BASE=/var/lib/parallax/parallax.db
Environment=PARALLAX_ADRESSE=127.0.0.1:8080
ExecStart=/opt/parallax/parallax
Restart=on-failure
RestartSec=5
TimeoutStopSec=15

[Install]
WantedBy=multi-user.target
```

`TimeoutStopSec` légèrement au-dessus des 10 secondes que le binaire
s'accorde lui-même pour finir les requêtes en cours (§5) : systemd doit
laisser l'arrêt propre aller à son terme avant de couper au signal KILL.

Créer l'utilisateur système et les droits, puis activer :

```sh
sudo useradd --system --home /var/lib/parallax --shell /usr/sbin/nologin parallax
sudo mkdir -p /var/lib/parallax && sudo chown parallax:parallax /var/lib/parallax
sudo systemctl daemon-reload
sudo systemctl enable --now parallax
sudo systemctl status parallax
```

## 5. Arrêt propre et supervision

Le binaire intercepte SIGTERM (ce qu'envoie `systemctl stop`) et SIGINT
(Ctrl+C) : il cesse d'accepter de nouvelles requêtes, laisse jusqu'à 10
secondes aux requêtes en cours pour se terminer, puis coupe. Un
redémarrage ou un déploiement ne tranche pas une écriture en cours.

`GET /sante` répond `200 ok` sans authentification si la base répond, `503`
sinon — à sonder par le reverse proxy ou un `ExecStartPost` systemd.

## 6. Sauvegarde

### 6.1 Copie locale à la demande

`-sauvegarde` produit une copie cohérente sans interrompre le service
(`VACUUM INTO`, deux processus distincts peuvent ouvrir le même fichier
SQLite en WAL) :

```sh
/opt/parallax/parallax -base /var/lib/parallax/parallax.db \
  -sauvegarde /var/backups/parallax/parallax-$(date +%F).db
```

Utilisable en cron si l'on ne veut pas du dépôt S3 intégré (§6.2) :

```cron
0 2 * * * parallax /opt/parallax/parallax -base /var/lib/parallax/parallax.db -sauvegarde /var/backups/parallax/parallax-$(date +\%F).db
```

### 6.1 bis Sauvegarde locale automatique (v3.7)

Intégrée au binaire, réglée depuis l'écran **Sauvegarde**
(`/parametres/sauvegarde`, administrateur) plutôt que par variable
d'environnement : dossier de destination, heure quotidienne et rétention
en jours. Tant qu'aucun dossier ou aucune heure n'est réglé, elle reste
désactivée — aucun défaut choisi à la place de l'administrateur.

Chaque jour à l'heure réglée, une nouvelle copie n'est écrite que si le
journal des modifications signale une activité depuis la précédente
réussite (`depot.ActiviteDepuis`) — un soir sans écriture métier ne produit
pas de fichier redondant. Les copies plus vieilles que la rétention réglée
sont supprimées automatiquement, quel que soit le résultat du jour. Un
bouton **Sauvegarder maintenant** écrit une copie immédiatement, pratique
pour vérifier que le dossier choisi est bien accessible en écriture sans
attendre l'heure programmée.

Mécanisme indépendant du dépôt S3 périodique (§6.2) : les deux peuvent
tourner en même temps, chacun avec son propre calendrier.

### 6.2 Dépôt périodique sur le stockage objet S3 interne

Intégré au binaire (paquet `internal/sauvegarde`, client SigV4 sans SDK).
Cible : un stockage objet compatible S3 **local** (MinIO ou équivalent),
en HTTPS avec l'autorité de certification interne. Le service démarre même
si le stockage est injoignable (principe « pas de dépendance réseau au
démarrage ») : l'échec est journalisé et retenté au cycle suivant.

| Variable | Défaut | Rôle |
|---|---|---|
| `PARALLAX_S3_ENDPOINT` | — | URL complète, schéma obligatoire : `https://objets.interne:9000` |
| `PARALLAX_S3_BUCKET` | — | bucket de destination |
| `PARALLAX_S3_PREFIXE` | `parallax/` | préfixe des clés |
| `PARALLAX_S3_REGION` | `us-east-1` | région signée (valeur attendue par la plupart des S3 locaux) |
| `PARALLAX_S3_CLE_ACCES` | — | identifiant d'accès |
| `PARALLAX_S3_CLE_SECRETE` | — | clé secrète |
| `PARALLAX_S3_CA` | — | fichier PEM de l'autorité de certification interne (sinon, magasin système) |
| `PARALLAX_S3_STYLE_VIRTUEL` | `0` | `1` pour l'adressage `bucket.endpoint` ; par défaut `endpoint/bucket` (MinIO) |
| `PARALLAX_SAUVEGARDE_INTERVALLE` | `24h` | période du dépôt (`0` désactive) |

Le dépôt est actif dès que endpoint, bucket et les deux clés sont
renseignés. Chaque cycle : `VACUUM INTO` un fichier temporaire, envoi sous
la clé `<préfixe>parallax-AAAAMMJJ-HHMMSS.db`, suppression du temporaire.
Pas de purge côté application : la rétention se règle sur le bucket
(politique de cycle de vie), là où elle est auditée.

`-sauvegarde-s3` exécute un dépôt immédiatement puis quitte — pour valider
la configuration avant de laisser tourner le service :

```sh
sudo -u parallax /opt/parallax/parallax -sauvegarde-s3
```

**Les clés ne vont jamais en base** (la base est précisément ce qu'on
sauvegarde, et tout lecteur de la base lirait les clés de son propre dépôt
de sauvegardes). Elles vivent dans un fichier d'environnement lisible du
seul service :

```sh
sudo install -m 0600 -o root -g parallax /dev/null /etc/parallax/secrets
sudo tee /etc/parallax/secrets >/dev/null <<'EOF'
PARALLAX_S3_ENDPOINT=https://objets.interne:9000
PARALLAX_S3_BUCKET=sauvegardes-parallax
PARALLAX_S3_CA=/etc/parallax/ca-interne.pem
PARALLAX_S3_CLE_ACCES=…
PARALLAX_S3_CLE_SECRETE=…
EOF
```

et dans l'unité systemd (§4), sous `[Service]` :

```ini
EnvironmentFile=/etc/parallax/secrets
```

### 6.2 bis Derrière un reverse proxy TLS

Quand un proxy termine le TLS et parle à Parallax en HTTP simple, poser
`PARALLAX_COOKIES_SECURE=1` dans le fichier d'environnement : le cookie de
session porte alors l'attribut Secure même si la requête reçue n'est pas
chiffrée. Sans proxy et en HTTP interne, laisser absent.

### 6.3 Authentification LDAP (v3.6)

Facultative : sans `PARALLAX_LDAP_URL`, seuls les comptes locaux existent.
Bind simple LDAPv3 sur TLS, avec l'identité de l'utilisateur — aucun
compte de service, donc aucun secret à ajouter au fichier d'environnement.

| Variable | Rôle | Exemple |
|---|---|---|
| `PARALLAX_LDAP_URL` | annuaire, obligatoirement `ldaps://` | `ldaps://annuaire.exemple.org:636` |
| `PARALLAX_LDAP_DN` | gabarit du DN de bind, `{login}` remplacé par l'identifiant saisi | `uid={login},ou=people,dc=exemple,dc=org` |
| `PARALLAX_LDAP_CA` | autorité de certification interne, fichier PEM lisible par le service | `/etc/parallax/ca-interne.pem` |
| `PARALLAX_LDAP_DELAI` | délai de connexion et de réponse, défaut `5s` | `5s` |

Un compte créé par une connexion LDAP naît en LECTEUR ; un administrateur
le promeut. Garder au moins un administrateur local : l'annuaire coupé ou
mal configuré, il reste le seul accès.

## 7. Checklist de bascule

- [ ] Compte administrateur créé, mot de passe changé, comptes des 3-4
      rédacteurs créés avec le bon rôle
- [ ] Import des référentiels, du catalogue matériel et de l'inventaire réel
      (écran `/import`, ou saisie manuelle si le volume le permet)
- [ ] Recette des formules contre l'Excel actuel sur au moins trois clusters
      de technologies différentes — voir la section « Après l'import » de
      [`import-format.md`](import-format.md#après-limport)
- [ ] Sauvegarde automatisée vérifiée par une restauration test
- [ ] Formation des rédacteurs sur le constructeur de vues (c'est lui qui
      remplace les tableaux croisés dynamiques au quotidien)
- [ ] Bascule effective, ancien classeur Excel gelé en lecture seule
