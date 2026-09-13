#!/usr/bin/env python3
"""Validation du schéma Parallax par exécution réelle sous SQLite.

Charge les migrations, insère un jeu de données représentatif, puis exécute
les requêtes difficiles du produit :

  1. résolution d'une variable par (scénario, année) et portée hiérarchique
  2. agrégation de l'offre d'un cluster à une date donnée
  3. état du parc à une date passée, avec les caractéristiques matérielles
     de l'époque (immuabilité des révisions)
  4. détection de chevauchement entre règles d'une même métrique
  5. vérification des invariants déclarés dans modele-donnees.md

Usage :  python3 spec/validate_schema.py
"""

import sqlite3
import sys
from pathlib import Path

RACINE = Path(__file__).resolve().parent.parent
MIGRATIONS = RACINE / "internal" / "db" / "migrations"

echecs = []
succes = []


def verifier(nom, condition, detail=""):
    if condition:
        succes.append(nom)
    else:
        echecs.append(f"{nom} — {detail}")


def ouvrir():
    cx = sqlite3.connect(":memory:")
    cx.execute("PRAGMA foreign_keys = ON")
    cx.row_factory = sqlite3.Row
    for fichier in sorted(MIGRATIONS.glob("*.sql")):
        cx.executescript(fichier.read_text(encoding="utf-8"))
    return cx


# --------------------------------------------------------------- fixtures

def charger(cx):
    c = cx.cursor()

    c.executescript("""
    INSERT INTO projet (id, code, libelle) VALUES
        (1, 'LOGS', 'Log Management Production'),
        (2, 'STREAM', 'Stream');

    INSERT INTO environnement (id, code, libelle, ordre) VALUES
        (1, 'PROD', 'Production', 10),
        (2, 'QUAL', 'Qualification', 20);

    INSERT INTO techno (id, code, libelle) VALUES
        (1, 'ELASTIC', 'Elasticsearch'),
        (2, 'KAFKA', 'Kafka');

    INSERT INTO tier (id, code, libelle, ordre) VALUES
        (1, 'HOT', 'Hot', 10),
        (2, 'COLD', 'Cold', 30);

    INSERT INTO usage_fonctionnel (id, code, libelle) VALUES
        (1, 'LOGMGMT', 'Log Management');

    INSERT INTO zone (id, code, libelle) VALUES
        (1, 'DC1', 'Zone 1'),
        (2, 'DC2', 'Zone 2');

    -- clusters
    INSERT INTO cluster (id, nom, projet_id, environnement_id, techno_id, tier_id, usage_fonctionnel_id) VALUES
        (1, 'ElasticHot',  1, 1, 1, 1, 1),
        (2, 'ElasticCold1',1, 1, 1, 2, 1),
        (3, 'Kafka',       1, 1, 2, NULL, 1),
        (4, 'ElasticHot',  1, 2, 1, 1, 1);

    -- catalogue : deux générations
    INSERT INTO modele (id, type, annee, code, mode_financement, duree_lease_mois,
                        date_debut_lease, prix_fournisseur_ht, cout_annuel_ht, duree_cout_annees) VALUES
        (1, 'STD',     2020, 'STD-2020',     'LEASE', 60, '2020-06-01', 18000, 4200, 5),
        (2, 'DENSE',  2025, 'DENSE-2025',  'LEASE', 60, '2025-04-01', 42000, 9800, 5);

    INSERT INTO revision (id, modele_id, numero, libelle, date_effet) VALUES
        (1, 1, 1, 'origine', '2020-06-01'),
        (2, 1, 2, 'ajout 8 SSD 3,84 To', '2024-03-01'),
        (3, 2, 1, 'origine', '2025-04-01');

    -- STD révision 1 : 40 coeurs, 768 Go, 12x8 To SSD, 2x10 Gbps
    -- (cpu et ram sont des scalaires : quantité 1, la capacité porte le total)
    INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite) VALUES
        (1, 'CPU',         'cpu',       1,  40, 'CORE'),
        (1, 'CPU',         'specrate',  1, 210, 'POINT'),
        (1, 'RAM',         'ram',       1, 768, 'GO'),
        (1, 'DISQUE_DATA', 'ssd',      12,   8, 'TO'),
        (1, 'NIC',         'nic',       2,  10, 'GBPS'),
        -- STD révision 2 : mêmes CPU/RAM, disques portés à 20 unités
        (2, 'CPU',         'cpu',       1,  40, 'CORE'),
        (2, 'CPU',         'specrate',  1, 210, 'POINT'),
        (2, 'RAM',         'ram',       1, 768, 'GO'),
        (2, 'DISQUE_DATA', 'ssd',      20,   8, 'TO'),
        (2, 'NIC',         'nic',       2,  10, 'GBPS'),
        -- DENSE : 64 coeurs, 1024 Go, 24x24 To HDD + 2x3,84 To SSD, 4 GPU
        (3, 'CPU',         'cpu',       1,  64, 'CORE'),
        (3, 'CPU',         'specrate',  1, 480, 'POINT'),
        (3, 'RAM',         'ram',       1, 1024, 'GO'),
        (3, 'DISQUE_DATA', 'hdd',      24,  24, 'TO'),
        (3, 'DISQUE_DATA', 'ssd',       2, 3.84, 'TO'),
        (3, 'NIC',         'nic',       2,  25, 'GBPS'),
        (3, 'GPU',         'gpu',       4,   1, 'UNITE'),
        (3, 'GPU',         'gpu_ram',   4,  80, 'GO'),
        (3, 'GPU',         'gpu_fp8',   4, 1979, 'TFLOPS'),
        (3, 'GPU',         'gpu_bp',    4, 3.35, 'TOS');

    INSERT INTO modele_noeud (revision_id, techno_id, nb_noeuds) VALUES
        (1, 1, 4), (1, 2, 4),
        (2, 1, 4), (2, 2, 4),
        (3, 1, 6), (3, 2, 6);

    INSERT INTO scenario (id, nom, projet_id, description, statut, date_creation) VALUES
        (1, 'Hypothese 2027 achat', 1, 'Achat de DENSE en 2027', 'ACTIF', '2026-09-01');

    -- serveurs réels
    INSERT INTO serveur (id, physical_name, hostname, zone_id, statut, date_entree) VALUES
        (1, 'PHY001', 'esh01', 1, 'EN_SERVICE', '2020-07-01'),
        (2, 'PHY002', 'esh02', 2, 'EN_SERVICE', '2020-07-01'),
        (3, 'PHY003', 'esc01', 1, 'EN_SERVICE', '2025-05-01');

    -- serveur hypothétique porté par le scénario
    INSERT INTO serveur (id, physical_name, zone_id, statut, scenario_id, date_entree) VALUES
        (4, NULL, 1, 'HYPOTHESE', 1, '2027-01-01');

    -- rattachement daté aux révisions : PHY001 passe en révision 2 au 01/03/2024
    INSERT INTO serveur_revision (serveur_id, revision_id, date_debut, date_fin) VALUES
        (1, 1, '2020-07-01', '2024-02-29'),
        (1, 2, '2024-03-01', NULL),
        (2, 1, '2020-07-01', NULL),
        (3, 3, '2025-05-01', NULL),
        (4, 3, '2027-01-01', NULL);

    -- affectations : PHY002 quitte ElasticHot pour Kafka en 2024
    INSERT INTO affectation (serveur_id, cluster_id, date_debut, date_fin) VALUES
        (1, 1, '2020-07-01', NULL),
        (2, 1, '2020-07-01', '2024-06-30'),
        (2, 3, '2024-07-01', NULL),
        (3, 2, '2025-05-01', NULL);

    INSERT INTO affectation (serveur_id, cluster_id, date_debut, scenario_id) VALUES
        (4, 1, '2027-01-01', 1);

    -- variables
    INSERT INTO variable (id, code, libelle, unite, defaut) VALUES
        (1, 'debit_jour_to',       'Débit journalier entrant', 'TO',  NULL),
        (2, 'retention_jours',     'Rétention',                'J',   NULL),
        (3, 'taux_compression',    'Taux de compression',      NULL,  1.0),
        (4, 'taux_remplissage_max','Remplissage max disque',   NULL,  0.7),
        (5, 'coef_formatage',      'Coefficient de formatage', NULL,  0.9);

    INSERT INTO metrique (id, code, libelle, unite) VALUES
        (1, 'DISQUE_UTILE_TO', 'Disque utile', 'TO'),
        (2, 'CPU_CORES',       'Coeurs CPU',   'CORE');
    """)

    # débit : saisi une seule fois au niveau projet + environnement
    c.executemany(
        """INSERT INTO variable_valeur
           (variable_id, scenario_id, annee, projet_id, environnement_id,
            techno_id, tier_id, cluster_id, valeur, modifie_le)
           VALUES (?,?,?,?,?,?,?,?,?, '2026-09-01')""",
        [
            # réel 2026 : 50 To/j au niveau LOGS/PROD
            (1, None, 2026, 1, 1, None, None, None, 50.0),
            # réel 2027 : 65 To/j
            (1, None, 2027, 1, 1, None, None, None, 65.0),
            # le scénario surcharge 2027 à 150 To/j
            (1, 1,    2027, 1, 1, None, None, None, 150.0),
            # rétention : spécialisée par tier
            (2, None, 2026, 1, 1, 1, 1, None, 7.0),    # HOT  = 7 j
            (2, None, 2026, 1, 1, 1, 2, None, 90.0),   # COLD = 90 j
            # compression au niveau techno
            (3, None, 2026, 1, 1, 1, None, None, 0.35),
            # surcharge la plus spécifique : ce cluster précis
            (3, None, 2026, 1, 1, 1, None, 2, 0.25),
        ],
    )

    # règles
    c.executemany(
        """INSERT INTO regle (id, nom, metrique_id, expression, niveau_evaluation,
                              composant_offre, projet_id, environnement_id, techno_id,
                              tier_id, usage_fonctionnel_id, cluster_id, date_creation)
           VALUES (?,?,?,?,?,?,?,?,?,?,?,?, '2026-09-01')""",
        [
            (1, 'Disque Elastic HOT', 1,
             'debit_jour_to * retention_jours * taux_compression',
             'PERIMETRE', 'ssd', 1, 1, 1, 1, None, None),
            (2, 'Disque Elastic COLD', 1,
             'debit_jour_to * retention_jours * taux_compression',
             'PERIMETRE', 'ssd', 1, 1, 1, 2, None, None),
            (3, 'CPU Elastic', 2,
             'debit_jour_to * 1.5',
             'PERIMETRE', 'cpu', 1, 1, 1, None, None, None),
        ],
    )

    cx.commit()


# ----------------------------------------------- 1. résolution de variable

SQL_RESOLUTION = """
WITH ctx AS (
    SELECT c.id AS cluster_id, c.projet_id, c.environnement_id,
           c.techno_id, c.tier_id
    FROM cluster c WHERE c.id = :cluster_id
)
SELECT vv.valeur,
       -- spécificité : plus la portée est fine, plus le poids est élevé
       (CASE WHEN vv.cluster_id       IS NOT NULL THEN 16 ELSE 0 END
      + CASE WHEN vv.tier_id          IS NOT NULL THEN  8 ELSE 0 END
      + CASE WHEN vv.techno_id        IS NOT NULL THEN  4 ELSE 0 END
      + CASE WHEN vv.environnement_id IS NOT NULL THEN  2 ELSE 0 END
      + CASE WHEN vv.projet_id        IS NOT NULL THEN  1 ELSE 0 END) AS specificite,
       CASE WHEN vv.scenario_id IS NULL THEN 0 ELSE 1 END AS prio_scenario
FROM variable_valeur vv
JOIN variable v ON v.id = vv.variable_id
CROSS JOIN ctx
WHERE v.code = :code
  AND vv.annee = :annee
  AND (vv.scenario_id IS NULL OR vv.scenario_id = :scenario_id)
  AND (vv.projet_id        IS NULL OR vv.projet_id        = ctx.projet_id)
  AND (vv.environnement_id IS NULL OR vv.environnement_id = ctx.environnement_id)
  AND (vv.techno_id        IS NULL OR vv.techno_id        = ctx.techno_id)
  AND (vv.tier_id          IS NULL OR vv.tier_id          = ctx.tier_id)
  AND (vv.cluster_id       IS NULL OR vv.cluster_id       = ctx.cluster_id)
ORDER BY prio_scenario DESC, specificite DESC
LIMIT 1
"""


def resoudre(cx, code, cluster_id, annee, scenario_id=None):
    r = cx.execute(SQL_RESOLUTION, {
        "code": code, "cluster_id": cluster_id,
        "annee": annee, "scenario_id": scenario_id if scenario_id else -1,
    }).fetchone()
    if r:
        return r["valeur"]
    d = cx.execute("SELECT defaut FROM variable WHERE code = ?", (code,)).fetchone()
    return d["defaut"] if d else None


def test_resolution(cx):
    verifier("débit hérité du niveau projet/env (HOT 2026)",
             resoudre(cx, "debit_jour_to", 1, 2026) == 50.0,
             f"obtenu {resoudre(cx, 'debit_jour_to', 1, 2026)}")

    verifier("même débit hérité par le cluster COLD (saisi une seule fois)",
             resoudre(cx, "debit_jour_to", 2, 2026) == 50.0,
             f"obtenu {resoudre(cx, 'debit_jour_to', 2, 2026)}")

    verifier("rétention spécialisée par tier : HOT = 7",
             resoudre(cx, "retention_jours", 1, 2026) == 7.0)
    verifier("rétention spécialisée par tier : COLD = 90",
             resoudre(cx, "retention_jours", 2, 2026) == 90.0)

    verifier("surcharge la plus spécifique gagne (cluster 2 : 0.25 et non 0.35)",
             resoudre(cx, "taux_compression", 2, 2026) == 0.25,
             f"obtenu {resoudre(cx, 'taux_compression', 2, 2026)}")
    verifier("cluster non surchargé reçoit la valeur techno (0.35)",
             resoudre(cx, "taux_compression", 1, 2026) == 0.35)

    verifier("le scénario surcharge le réel (2027 : 150 et non 65)",
             resoudre(cx, "debit_jour_to", 1, 2027, scenario_id=1) == 150.0,
             f"obtenu {resoudre(cx, 'debit_jour_to', 1, 2027, 1)}")
    verifier("hors scénario, le réel s'applique (2027 : 65)",
             resoudre(cx, "debit_jour_to", 1, 2027) == 65.0)

    verifier("valeur par défaut si aucune valeur saisie",
             resoudre(cx, "taux_remplissage_max", 1, 2026) == 0.7)


# ------------------------------------------------- 2 & 3. offre et à-date

SQL_OFFRE = """
SELECT COALESCE(SUM(comp.quantite * comp.capacite_unitaire), 0) AS total,
       COUNT(DISTINCT s.id) AS nb_serveurs
FROM affectation a
JOIN serveur s          ON s.id = a.serveur_id
JOIN serveur_revision sr ON sr.serveur_id = s.id
                        AND sr.date_debut <= :date
                        AND (sr.date_fin IS NULL OR sr.date_fin >= :date)
JOIN composant comp      ON comp.revision_id = sr.revision_id
                        AND comp.code = :composant
WHERE a.cluster_id = :cluster_id
  AND a.date_debut <= :date
  AND (a.date_fin IS NULL OR a.date_fin >= :date)
  AND (a.scenario_id IS NULL OR a.scenario_id = :scenario_id)
  AND (s.scenario_id IS NULL OR s.scenario_id = :scenario_id)
"""


def offre(cx, cluster_id, composant, date, scenario_id=None):
    r = cx.execute(SQL_OFFRE, {
        "cluster_id": cluster_id, "composant": composant, "date": date,
        "scenario_id": scenario_id if scenario_id else -1,
    }).fetchone()
    return r["total"], r["nb_serveurs"]


def test_offre(cx):
    # au 01/01/2023 : PHY001 et PHY002 sur ElasticHot, tous deux en révision 1
    # 2 serveurs x 12 x 8 To = 192 To
    total, nb = offre(cx, 1, "ssd", "2023-01-01")
    verifier("offre à une date passée, caractéristiques d'époque",
             (total, nb) == (192.0, 2), f"obtenu {total} To / {nb} serveurs")

    # au 01/01/2026 : PHY002 est parti sur Kafka, PHY001 est passé en révision 2
    # 1 serveur x 20 x 8 To = 160 To
    total, nb = offre(cx, 1, "ssd", "2026-01-01")
    verifier("la révision postérieure est prise en compte à date courante",
             (total, nb) == (160.0, 1), f"obtenu {total} To / {nb} serveurs")

    # le serveur hypothétique n'apparaît pas hors de son scénario
    total_reel, nb_reel = offre(cx, 1, "ssd", "2027-06-01")
    total_scen, nb_scen = offre(cx, 1, "ssd", "2027-06-01", scenario_id=1)
    verifier("le serveur hypothétique est invisible dans le réel",
             nb_reel == 1, f"obtenu {nb_reel} serveurs")
    # DENSE porte 2 SSD de 3,84 To (ses 24 HDD de 24 To sont sous le code hdd,
    # invisible à une règle dont composant_offre = ssd)
    verifier("le serveur hypothétique apparaît dans son scénario",
             nb_scen == 2 and abs(total_scen - (160.0 + 2 * 3.84)) < 1e-9,
             f"obtenu {total_scen} To / {nb_scen} serveurs")
    total_hdd, _ = offre(cx, 1, "hdd", "2027-06-01", scenario_id=1)
    verifier("hdd et ssd sont deux offres distinctes",
             total_hdd == 24 * 24, f"obtenu {total_hdd} To de HDD")

    # PHY002 est bien passé sur Kafka
    _, nb_kafka = offre(cx, 3, "ssd", "2026-01-01")
    verifier("réaffectation d'un serveur vers un autre cluster",
             nb_kafka == 1, f"obtenu {nb_kafka}")


# ------------------------------------------ 4. chevauchement entre règles

SQL_CLUSTERS_MATCHES = """
SELECT c.id
FROM cluster c, regle r
WHERE r.id = :regle_id
  AND (r.projet_id            IS NULL OR r.projet_id            = c.projet_id)
  AND (r.environnement_id     IS NULL OR r.environnement_id     = c.environnement_id)
  AND (r.techno_id            IS NULL OR r.techno_id            = c.techno_id)
  AND (r.tier_id              IS NULL OR r.tier_id              = c.tier_id)
  AND (r.usage_fonctionnel_id IS NULL OR r.usage_fonctionnel_id = c.usage_fonctionnel_id)
  AND (r.cluster_id           IS NULL OR r.cluster_id           = c.id)
"""


def clusters_matches(cx, regle_id):
    return {r["id"] for r in cx.execute(SQL_CLUSTERS_MATCHES, {"regle_id": regle_id})}


def conflits(cx, metrique_id):
    regles = [r["id"] for r in cx.execute(
        "SELECT id FROM regle WHERE metrique_id = ? AND actif = 1", (metrique_id,))]
    out = []
    for i, a in enumerate(regles):
        for b in regles[i + 1:]:
            commun = clusters_matches(cx, a) & clusters_matches(cx, b)
            if commun:
                out.append((a, b, sorted(commun)))
    return out


def test_conflits(cx):
    verifier("règle HOT ne matche que le cluster HOT de PROD",
             clusters_matches(cx, 1) == {1}, f"obtenu {clusters_matches(cx, 1)}")
    verifier("pas de conflit entre les règles disque HOT et COLD",
             conflits(cx, 1) == [], f"obtenu {conflits(cx, 1)}")

    # on ajoute une règle générique Elastic sur la même métrique : elle
    # recouvre les deux règles précédentes, l'application doit le refuser
    cx.execute("""INSERT INTO regle (id, nom, metrique_id, expression, niveau_evaluation,
                                     composant_offre, projet_id, environnement_id, techno_id,
                                     date_creation)
                  VALUES (99, 'Disque Elastic generique', 1, 'debit_jour_to',
                          'PERIMETRE', 'ssd', 1, 1, 1, '2026-09-01')""")
    detectes = conflits(cx, 1)
    verifier("conflit détecté avec une règle générique recouvrante",
             len(detectes) == 2, f"obtenu {detectes}")
    cx.execute("DELETE FROM regle WHERE id = 99")


# --------------------------------------------------- 5. invariants du schéma

def test_invariants(cx):
    # invariant 5 : HYPOTHESE sans scénario doit être refusé
    try:
        cx.execute("INSERT INTO serveur (statut) VALUES ('HYPOTHESE')")
        verifier("serveur HYPOTHESE sans scénario refusé", False, "insertion acceptée")
        cx.rollback()
    except sqlite3.IntegrityError:
        verifier("serveur HYPOTHESE sans scénario refusé", True)

    # unicité de portée d'une valeur de variable
    try:
        cx.execute("""INSERT INTO variable_valeur
            (variable_id, scenario_id, annee, projet_id, environnement_id,
             techno_id, tier_id, cluster_id, valeur, modifie_le)
            VALUES (1, NULL, 2026, 1, 1, NULL, NULL, NULL, 99, '2026-09-01')""")
        verifier("doublon de portée de variable refusé", False, "insertion acceptée")
        cx.rollback()
    except sqlite3.IntegrityError:
        verifier("doublon de portée de variable refusé", True)

    # statut invalide
    try:
        cx.execute("INSERT INTO serveur (statut) VALUES ('N_IMPORTE_QUOI')")
        verifier("statut de serveur hors énumération refusé", False, "insertion acceptée")
        cx.rollback()
    except sqlite3.IntegrityError:
        verifier("statut de serveur hors énumération refusé", True)

    # date_fin antérieure à date_debut
    try:
        cx.execute("""INSERT INTO affectation (serveur_id, cluster_id, date_debut, date_fin)
                      VALUES (1, 1, '2025-01-01', '2024-01-01')""")
        verifier("période incohérente refusée", False, "insertion acceptée")
        cx.rollback()
    except sqlite3.IntegrityError:
        verifier("période incohérente refusée", True)

    # intégrité référentielle
    try:
        cx.execute("""INSERT INTO cluster (nom, projet_id, environnement_id, techno_id)
                      VALUES ('X', 999, 1, 1)""")
        verifier("clé étrangère inexistante refusée", False, "insertion acceptée")
        cx.rollback()
    except sqlite3.IntegrityError:
        verifier("clé étrangère inexistante refusée", True)

    # invariant 3 (migration 0002) — PHY001 a déjà une affectation active sur
    # ElasticHot dans le réel ; une seconde affectation active est refusée.
    try:
        cx.execute("""INSERT INTO affectation (serveur_id, cluster_id, date_debut)
                      VALUES (1, 3, '2025-01-01')""")
        verifier("seconde affectation active du même serveur refusée", False, "insertion acceptée")
        cx.rollback()
    except sqlite3.IntegrityError:
        verifier("seconde affectation active du même serveur refusée", True)

    # ... mais la même affectation active est permise dans un scénario, en
    # parallèle du réel : les seaux (réel / scénario) sont distincts.
    try:
        cx.execute("""INSERT INTO affectation (serveur_id, cluster_id, date_debut, scenario_id)
                      VALUES (1, 3, '2025-01-01', 1)""")
        verifier("affectation active portée par un scénario permise en parallèle du réel", True)
        cx.rollback()
    except sqlite3.IntegrityError as e:
        verifier("affectation active portée par un scénario permise en parallèle du réel",
                 False, str(e))

    # invariant 4 (migration 0002) — PHY002 a un rattachement de révision ouvert ;
    # un second rattachement ouvert est refusé.
    try:
        cx.execute("""INSERT INTO serveur_revision (serveur_id, revision_id, date_debut)
                      VALUES (2, 3, '2025-01-01')""")
        verifier("second rattachement de révision ouvert refusé", False, "insertion acceptée")
        cx.rollback()
    except sqlite3.IntegrityError:
        verifier("second rattachement de révision ouvert refusé", True)


# --------------------------------------------------------------------- main

def main():
    if not MIGRATIONS.exists():
        print(f"migrations introuvables : {MIGRATIONS}", file=sys.stderr)
        return 2

    cx = ouvrir()
    charger(cx)

    test_resolution(cx)
    test_offre(cx)
    test_conflits(cx)
    test_invariants(cx)

    print(f"\n{len(succes)} vérifications passées")
    for s in succes:
        print(f"  ok   {s}")
    if echecs:
        print(f"\n{len(echecs)} ÉCHECS")
        for e in echecs:
            print(f"  FAIL {e}")
        return 1
    print("\nSchéma validé.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
