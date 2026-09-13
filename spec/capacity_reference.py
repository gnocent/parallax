#!/usr/bin/env python3
"""Implémentation de référence du moteur de capacité Parallax.

Ce fichier est une SPÉCIFICATION EXÉCUTABLE, pas du code de production.
Il définit sans ambiguïté :

  - la résolution d'une variable par (scénario, année) et portée hiérarchique
  - le matching d'une règle sur un cluster et la détection de chevauchement
  - l'ordre exact du dimensionnement et l'application des contraintes

Il produit `spec/vectors.json`, consommé tel quel par les tests Go.
Toute divergence entre l'implémentation Go et ces vecteurs est un bug Go,
sauf décision explicite de changer la spécification — auquel cas on modifie
ce fichier d'abord et on régénère les vecteurs.

Usage :  python3 spec/capacity_reference.py [--check]
"""

import argparse
import ast
import json
import math
import operator
from pathlib import Path

RACINE = Path(__file__).resolve().parent.parent
VECTEURS = RACINE / "spec" / "vectors.json"

# Ordre de spécificité des portées, du plus général au plus spécifique.
# Le poids est une puissance de 2 : une portée plus fine l'emporte toujours
# sur n'importe quelle combinaison de portées plus grossières.
PORTEES = ["projet_id", "environnement_id", "techno_id", "tier_id", "cluster_id"]
POIDS = {nom: 1 << i for i, nom in enumerate(PORTEES)}

DIMENSIONS_REGLE = [
    "projet_id", "environnement_id", "techno_id",
    "tier_id", "usage_fonctionnel_id", "cluster_id",
]


# =========================================================== évaluateur

_BINAIRES = {
    ast.Add: operator.add, ast.Sub: operator.sub,
    ast.Mult: operator.mul, ast.Div: operator.truediv,
    ast.Mod: operator.mod, ast.Pow: operator.pow,
}
_UNAIRES = {ast.UAdd: operator.pos, ast.USub: operator.neg}

def _arrondi(x):
    """Arrondi au plus loin de zéro sur les demis (2,5 -> 3).

    Python arrondit au pair le plus proche (2,5 -> 2), Go arrondit au plus
    loin de zéro. On fixe explicitement la sémantique Go, plus intuitive pour
    un dimensionnement, et on l'impose ici pour éviter toute divergence.
    """
    return float(math.floor(x + 0.5)) if x >= 0 else float(math.ceil(x - 0.5))


_FONCTIONS = {
    "ceil": lambda x: float(math.ceil(x)),
    "floor": lambda x: float(math.floor(x)),
    "round": _arrondi,
    "abs": abs,
    "min": min,
    "max": max,
}


class ErreurExpression(Exception):
    pass


def evaluer(expression, variables):
    """Évalue une expression arithmétique restreinte.

    Volontairement limité aux opérations que `expr-lang/expr` fournit en Go :
    arithmétique, ceil, floor, round, abs, min, max. Aucune structure de
    contrôle, aucun accès attribut, aucun appel arbitraire.
    """
    try:
        arbre = ast.parse(expression, mode="eval")
    except SyntaxError as e:
        raise ErreurExpression(f"syntaxe invalide : {e}") from e

    def visiter(n):
        if isinstance(n, ast.Expression):
            return visiter(n.body)
        if isinstance(n, ast.Constant):
            if isinstance(n.value, (int, float)) and not isinstance(n.value, bool):
                return float(n.value)
            raise ErreurExpression(f"constante non numérique : {n.value!r}")
        if isinstance(n, ast.Name):
            if n.id not in variables:
                raise ErreurExpression(f"variable inconnue : {n.id}")
            return float(variables[n.id])
        if isinstance(n, ast.BinOp):
            op = _BINAIRES.get(type(n.op))
            if op is None:
                raise ErreurExpression("opérateur binaire non autorisé")
            droite = visiter(n.right)
            if isinstance(n.op, (ast.Div, ast.Mod)) and droite == 0:
                raise ErreurExpression("division par zéro")
            return op(visiter(n.left), droite)
        if isinstance(n, ast.UnaryOp):
            op = _UNAIRES.get(type(n.op))
            if op is None:
                raise ErreurExpression("opérateur unaire non autorisé")
            return op(visiter(n.operand))
        if isinstance(n, ast.Call):
            if not isinstance(n.func, ast.Name) or n.func.id not in _FONCTIONS:
                raise ErreurExpression("fonction non autorisée")
            return float(_FONCTIONS[n.func.id](*[visiter(a) for a in n.args]))
        raise ErreurExpression(f"construction non autorisée : {type(n).__name__}")

    return visiter(arbre)


def variables_utilisees(expression):
    """Liste les identifiants référencés — sert à valider une règle à la saisie."""
    arbre = ast.parse(expression, mode="eval")
    return sorted({
        n.id for n in ast.walk(arbre)
        if isinstance(n, ast.Name) and n.id not in _FONCTIONS
    })


# ================================================ résolution de variable

def resoudre_variable(valeurs, code, cluster, annee, scenario_id=None, defaut=None):
    """Retourne la valeur applicable.

    Priorité : d'abord le scénario, puis le réel. À égalité de scénario,
    la portée la plus spécifique l'emporte. À défaut, la valeur par défaut
    de la variable.
    """
    candidates = []
    for v in valeurs:
        if v["code"] != code or v["annee"] != annee:
            continue
        if v.get("scenario_id") is not None and v["scenario_id"] != scenario_id:
            continue
        specificite = 0
        applicable = True
        for portee in PORTEES:
            attendu = v.get(portee)
            if attendu is None:
                continue
            # le cluster s'identifie par « id », les autres portées par leur
            # colonne homonyme
            valeur = cluster["id"] if portee == "cluster_id" else cluster.get(portee)
            if valeur != attendu:
                applicable = False
                break
            specificite += POIDS[portee]
        if not applicable:
            continue
        prio = 1 if v.get("scenario_id") is not None else 0
        candidates.append((prio, specificite, v["valeur"]))

    if not candidates:
        return defaut
    candidates.sort(key=lambda t: (t[0], t[1]), reverse=True)
    return candidates[0][2]


# ============================================ matching et chevauchement

def regle_matche(regle, cluster):
    """Une règle s'applique si chacun de ses critères est vide ou égal."""
    for dim in DIMENSIONS_REGLE:
        attendu = regle.get(dim)
        if attendu is None:
            continue
        valeur = cluster["id"] if dim == "cluster_id" else cluster.get(dim)
        if valeur != attendu:
            return False
    return True


def detecter_conflits(regles, clusters):
    """Deux règles actives d'une même métrique ne doivent pas matcher un
    même cluster réel. On raisonne sur les clusters existants, pas sur des
    combinaisons théoriques : c'est plus simple et plus utile à l'usage."""
    actives = [r for r in regles if r.get("actif", True)]
    conflits = []
    for i, a in enumerate(actives):
        for b in actives[i + 1:]:
            if a["metrique"] != b["metrique"]:
                continue
            communs = sorted(
                c["id"] for c in clusters
                if regle_matche(a, c) and regle_matche(b, c)
            )
            if communs:
                conflits.append({
                    "regle_a": a["id"], "regle_b": b["id"], "clusters": communs,
                })
    return conflits


# ==================================================== contraintes

def _multiple_superieur(n, m):
    return int(math.ceil(n / m) * m) if m and m > 0 else n


def appliquer_contraintes(nb_brut, contraintes):
    """Applique les contraintes du cluster à un nombre de serveurs brut.

    Ordre : arrondi supérieur, minimum total, multiple total, puis
    contraintes par zone. Les contraintes totales et par zone
    pouvant se contredire, on itère jusqu'au point fixe.
    """
    c = {k: v for k, v in contraintes.items() if v is not None}
    nb_zone = int(c.get("NB_ZONES", 0) or 0)
    min_total = int(c.get("MIN_TOTAL", 0) or 0)
    mult_total = int(c.get("MULTIPLE_TOTAL", 0) or 0)
    min_zone = int(c.get("MIN_PAR_ZONE", 0) or 0)
    mult_zone = int(c.get("MULTIPLE_PAR_ZONE", 0) or 0)
    equilibrage = bool(c.get("EQUILIBRAGE_ZONE", False))

    nb = int(math.ceil(nb_brut - 1e-9))
    par_zone = None
    repartition_forcee = nb_zone > 0 and (min_zone or mult_zone or equilibrage)

    for _ in range(100):
        precedent = nb
        if min_total:
            nb = max(nb, min_total)
        if mult_total:
            nb = _multiple_superieur(nb, mult_total)
        if repartition_forcee:
            par_zone = int(math.ceil(nb / nb_zone))
            if min_zone:
                par_zone = max(par_zone, min_zone)
            if mult_zone:
                par_zone = _multiple_superieur(par_zone, mult_zone)
            nb = par_zone * nb_zone
        if nb == precedent:
            break

    return {
        "nb_serveurs": nb,
        "par_zone": par_zone,
        "nb_zones": nb_zone or None,
    }


# ==================================================== dimensionnement

def dimensionner(cluster, regles, valeurs, annee, modele, scenario_id=None,
                 contraintes=None, agregats=None, serveurs=None, defauts=None,
                 offre_conservee=None):
    """Dimensionne un cluster pour un modèle candidat.

    Ordre imposé — ne pas réorganiser :
      1. besoin brut par règle applicable
      1b. besoin résiduel = max(0, besoin − offre conservée du composant)
      2. nombre de serveurs brut = besoin résiduel / capacité unitaire du modèle
      3. maximum sur les règles : la règle gagnante est le facteur limitant
      4. contraintes du cluster

    Arrondir avant le maximum donnerait un résultat différent et faux.

    offre_conservee (v2.3) : capacité, par code de composant, des serveurs
    déjà installés que l'hypothèse CONSERVE — une sélection choisie par
    l'utilisateur (par génération, typiquement), pas forcément tout le parc.
    Vide ou absente : renouvellement complet, le modèle candidat couvre tout
    le besoin. Les contraintes du cluster s'appliquent au nombre de serveurs
    à ajouter, pas au parc conservé.
    """
    contraintes = contraintes or {}
    agregats = agregats or {}
    defauts = defauts or {}
    offre_conservee = offre_conservee or {}
    applicables = [r for r in regles
                   if r.get("actif", True) and regle_matche(r, cluster)]

    details = []
    for regle in sorted(applicables, key=lambda r: r["id"]):
        base = dict(agregats)
        for code in variables_utilisees(regle["expression"]):
            if code in base:
                continue
            base[code] = resoudre_variable(
                valeurs, code, cluster, annee, scenario_id, defauts.get(code))
            if base[code] is None:
                raise ErreurExpression(
                    f"règle {regle['id']} : variable « {code} » non résolue")

        if regle["niveau_evaluation"] == "PAR_SERVEUR":
            besoin = 0.0
            for s in (serveurs or []):
                besoin += evaluer(regle["expression"], {**base, **s})
        else:
            besoin = evaluer(regle["expression"], base)

        capacite = modele["composants"].get(regle["composant_offre"])
        if not capacite:
            raise ErreurExpression(
                f"règle {regle['id']} : le modèle {modele['code']} ne fournit "
                f"pas « {regle['composant_offre']} »")

        residuel = max(0.0, besoin - offre_conservee.get(regle["composant_offre"], 0.0))
        details.append({
            "regle_id": regle["id"],
            "nom": regle["nom"],
            "metrique": regle["metrique"],
            "besoin": round(besoin, 6),
            "besoin_residuel": round(residuel, 6),
            "capacite_unitaire": capacite,
            "nb_brut": round(residuel / capacite, 6),
        })

    if not details:
        return {"nb_serveurs": 0, "regle_limitante": None, "details": [],
                "par_zone": None, "nb_zones": None,
                "cout_acquisition": 0.0, "cout_annuel": 0.0}

    limitante = max(details, key=lambda d: d["nb_brut"])
    resultat = appliquer_contraintes(limitante["nb_brut"], contraintes)
    nb = resultat["nb_serveurs"]

    return {
        **resultat,
        "regle_limitante": limitante["regle_id"],
        "details": details,
        "cout_acquisition": round(nb * modele.get("prix_fournisseur_ht", 0), 2),
        "cout_annuel": round(nb * modele.get("cout_annuel_ht", 0), 2),
    }


# ==================================================== vecteurs de test

def construire_vecteurs():
    cluster_hot = {"id": 1, "projet_id": 1, "environnement_id": 1,
                   "techno_id": 1, "tier_id": 1, "usage_fonctionnel_id": 1}
    cluster_cold = {"id": 2, "projet_id": 1, "environnement_id": 1,
                    "techno_id": 1, "tier_id": 2, "usage_fonctionnel_id": 1}
    cluster_kafka = {"id": 3, "projet_id": 1, "environnement_id": 1,
                     "techno_id": 2, "tier_id": None, "usage_fonctionnel_id": 1}
    clusters = [cluster_hot, cluster_cold, cluster_kafka]

    valeurs = [
        {"code": "debit_jour_to", "annee": 2027, "scenario_id": None,
         "projet_id": 1, "environnement_id": 1, "valeur": 65.0},
        {"code": "debit_jour_to", "annee": 2027, "scenario_id": 1,
         "projet_id": 1, "environnement_id": 1, "valeur": 150.0},
        {"code": "retention_jours", "annee": 2027, "scenario_id": None,
         "projet_id": 1, "environnement_id": 1, "techno_id": 1, "tier_id": 1,
         "valeur": 7.0},
        {"code": "retention_jours", "annee": 2027, "scenario_id": None,
         "projet_id": 1, "environnement_id": 1, "techno_id": 1, "tier_id": 2,
         "valeur": 90.0},
        {"code": "taux_compression", "annee": 2027, "scenario_id": None,
         "techno_id": 1, "valeur": 0.35},
        {"code": "taux_compression", "annee": 2027, "scenario_id": None,
         "cluster_id": 2, "valeur": 0.25},
        {"code": "replication", "annee": 2027, "scenario_id": None, "valeur": 2.0},
    ]
    defauts = {"coef_formatage": 0.9, "taux_remplissage_max": 0.7,
               "ram_max_licence": 64.0}

    modele_dense = {
        "code": "DENSE-2025", "prix_fournisseur_ht": 42000.0,
        "cout_annuel_ht": 9800.0,
        # 24 x 24 To bruts, corrigés du formatage et du remplissage max
        "composants": {"ssd": 24 * 24 * 0.9 * 0.7,
                       "cpu": 64.0, "ram": 1024.0},
    }
    modele_mid = {
        "code": "MID-2025", "prix_fournisseur_ht": 31000.0,
        "cout_annuel_ht": 7200.0,
        "composants": {"ssd": 12 * 16 * 0.9 * 0.7,
                       "cpu": 96.0, "ram": 768.0},
    }

    expr_disque = "debit_jour_to * retention_jours * taux_compression * replication"
    regles = [
        {"id": 1, "nom": "Disque Elastic HOT", "metrique": "DISQUE_UTILE_TO",
         "expression": expr_disque, "niveau_evaluation": "PERIMETRE",
         "composant_offre": "ssd", "actif": True,
         "projet_id": 1, "environnement_id": 1, "techno_id": 1, "tier_id": 1},
        {"id": 2, "nom": "Disque Elastic COLD", "metrique": "DISQUE_UTILE_TO",
         "expression": expr_disque, "niveau_evaluation": "PERIMETRE",
         "composant_offre": "ssd", "actif": True,
         "projet_id": 1, "environnement_id": 1, "techno_id": 1, "tier_id": 2},
        {"id": 3, "nom": "CPU Elastic", "metrique": "CPU_CORES",
         "expression": "debit_jour_to * 1.5", "niveau_evaluation": "PERIMETRE",
         "composant_offre": "cpu", "actif": True,
         "projet_id": 1, "environnement_id": 1, "techno_id": 1},
    ]
    regle_generique = {
        "id": 99, "nom": "Disque Elastic generique", "metrique": "DISQUE_UTILE_TO",
        "expression": "debit_jour_to", "niveau_evaluation": "PERIMETRE",
        "composant_offre": "ssd", "actif": True,
        "projet_id": 1, "environnement_id": 1, "techno_id": 1,
    }

    vecteurs = {"expressions": [], "resolution": [], "conflits": [],
                "contraintes": [], "dimensionnement": []}

    # ---- expressions
    for expression, variables in [
        ("2 + 3 * 4", {}),
        ("(2 + 3) * 4", {}),
        ("ceil(7 / 2)", {}),
        ("floor(7 / 2)", {}),
        ("max(3, 9, 4)", {}),
        ("min(3, 9, 4)", {}),
        ("ceil(debit * retention / 10)", {"debit": 65.0, "retention": 7.0}),
        ("a * b * c", {"a": 1.5, "b": 2.0, "c": 4.0}),
        ("-x + 10", {"x": 3.0}),
        ("abs(0 - 5) + round(2.6)", {}),
    ]:
        vecteurs["expressions"].append({
            "expression": expression, "variables": variables,
            "attendu": round(evaluer(expression, variables), 9),
        })

    for expression, variables, motif in [
        ("debit *", {}, "syntaxe"),
        ("inconnue + 1", {}, "variable inconnue"),
        ("1 / 0", {}, "division par zéro"),
        ("__import__('os')", {}, "fonction non autorisée"),
    ]:
        vecteurs["expressions"].append({
            "expression": expression, "variables": variables,
            "attendu": None, "erreur": motif,
        })

    # ---- résolution de variable
    for libelle, code, cluster, annee, scenario in [
        ("débit hérité du niveau projet/env", "debit_jour_to", cluster_hot, 2027, None),
        ("débit surchargé par le scénario", "debit_jour_to", cluster_hot, 2027, 1),
        ("rétention spécialisée HOT", "retention_jours", cluster_hot, 2027, None),
        ("rétention spécialisée COLD", "retention_jours", cluster_cold, 2027, None),
        ("compression au niveau techno", "taux_compression", cluster_hot, 2027, None),
        ("compression surchargée au cluster", "taux_compression", cluster_cold, 2027, None),
        ("variable globale sans portée", "replication", cluster_kafka, 2027, None),
        ("valeur par défaut", "coef_formatage", cluster_hot, 2027, None),
        ("année absente : défaut", "debit_jour_to", cluster_hot, 2030, None),
    ]:
        vecteurs["resolution"].append({
            "libelle": libelle, "code": code, "cluster": cluster,
            "annee": annee, "scenario_id": scenario,
            "valeurs": valeurs,
            "defaut": defauts.get(code),
            "attendu": resoudre_variable(valeurs, code, cluster, annee,
                                         scenario, defauts.get(code)),
        })

    # ---- conflits
    vecteurs["conflits"].append({
        "libelle": "règles HOT et COLD disjointes",
        "regles": regles, "clusters": clusters,
        "attendu": detecter_conflits(regles, clusters),
    })
    avec_generique = regles + [regle_generique]
    vecteurs["conflits"].append({
        "libelle": "règle générique recouvrante",
        "regles": avec_generique, "clusters": clusters,
        "attendu": detecter_conflits(avec_generique, clusters),
    })
    desactivee = regles + [{**regle_generique, "actif": False}]
    vecteurs["conflits"].append({
        "libelle": "règle recouvrante mais désactivée",
        "regles": desactivee, "clusters": clusters,
        "attendu": detecter_conflits(desactivee, clusters),
    })

    # ---- contraintes
    for libelle, brut, contraintes in [
        ("aucune contrainte, arrondi supérieur", 12.2, {}),
        ("valeur déjà entière", 12.0, {}),
        ("minimum total", 3.1, {"MIN_TOTAL": 6}),
        ("multiple total", 12.2, {"MULTIPLE_TOTAL": 3}),
        ("multiple total déjà satisfait", 12.0, {"MULTIPLE_TOTAL": 3}),
        ("équilibrage sur 3 zones", 10.0, {"NB_ZONES": 3, "EQUILIBRAGE_ZONE": 1}),
        ("équilibrage sur 2 zones", 9.0, {"NB_ZONES": 2, "EQUILIBRAGE_ZONE": 1}),
        ("minimum par zone", 4.0, {"NB_ZONES": 3, "MIN_PAR_ZONE": 3}),
        ("multiple par zone", 10.0, {"NB_ZONES": 2, "MULTIPLE_PAR_ZONE": 3}),
        ("total et par zone combinés",
         10.0, {"NB_ZONES": 3, "MULTIPLE_TOTAL": 4, "EQUILIBRAGE_ZONE": 1}),
        ("stretched 2 vers 3 zones",
         12.0, {"NB_ZONES": 3, "MIN_PAR_ZONE": 2, "MULTIPLE_PAR_ZONE": 2}),
        ("nb_zones sans contrainte par zone ne force rien",
         10.0, {"NB_ZONES": 3}),
    ]:
        vecteurs["contraintes"].append({
            "libelle": libelle, "nb_brut": brut, "contraintes": contraintes,
            "attendu": appliquer_contraintes(brut, contraintes),
        })

    # ---- dimensionnement complet
    cas = [
        ("HOT 2027 réel, DENSE, sans contrainte",
         cluster_hot, 2027, None, modele_dense, {}, {}),
        ("HOT 2027 sous scénario (débit 150)",
         cluster_hot, 2027, 1, modele_dense, {}, {}),
        ("HOT 2027 scénario avec équilibrage 3 zones",
         cluster_hot, 2027, 1, modele_dense,
         {"NB_ZONES": 3, "EQUILIBRAGE_ZONE": 1}, {}),
        ("HOT 2027 scénario, modèle alternatif MID",
         cluster_hot, 2027, 1, modele_mid, {}, {}),
        ("COLD 2027 réel, compression surchargée",
         cluster_cold, 2027, None, modele_dense, {}, {}),
        ("COLD 2027 avec minimum par zone",
         cluster_cold, 2027, None, modele_dense,
         {"NB_ZONES": 2, "MIN_PAR_ZONE": 4}, {}),
        ("Kafka : aucune règle applicable",
         cluster_kafka, 2027, None, modele_dense, {}, {}),
        # ---- renfort (v2.3) : une partie du parc est conservée
        ("HOT 2027 réel, renfort : 400 To de disque conservés",
         cluster_hot, 2027, None, modele_dense, {}, {"ssd": 400}),
        ("HOT 2027 réel, renfort partiel sur les deux composants",
         cluster_hot, 2027, None, modele_dense, {},
         {"ssd": 400, "cpu": 100}),
        ("HOT 2027 réel, renfort : l'offre conservée couvre tout le besoin",
         cluster_hot, 2027, None, modele_dense, {},
         {"ssd": 1e9, "cpu": 1e9}),
        ("HOT 2027 réel, renfort couvrant tout, minimum 2 imposé aux ajouts",
         cluster_hot, 2027, None, modele_dense, {"MIN_TOTAL": 2},
         {"ssd": 1e9, "cpu": 1e9}),
    ]
    for libelle, cluster, annee, scenario, modele, contraintes, conservee in cas:
        vecteurs["dimensionnement"].append({
            "libelle": libelle,
            "cluster": cluster, "annee": annee, "scenario_id": scenario,
            "modele": modele, "contraintes": contraintes,
            "offre_conservee": conservee,
            "regles": regles, "valeurs": valeurs, "defauts": defauts,
            "attendu": dimensionner(cluster, regles, valeurs, annee, modele,
                                    scenario, contraintes, defauts=defauts,
                                    offre_conservee=conservee),
        })

    return vecteurs


def main():
    parseur = argparse.ArgumentParser()
    parseur.add_argument("--check", action="store_true",
                         help="vérifie que vectors.json est à jour sans le réécrire")
    args = parseur.parse_args()

    vecteurs = construire_vecteurs()
    rendu = json.dumps(vecteurs, indent=2, ensure_ascii=False, sort_keys=True)

    if args.check:
        if not VECTEURS.exists():
            print("vectors.json absent")
            return 1
        if VECTEURS.read_text(encoding="utf-8") != rendu:
            print("vectors.json est désynchronisé de la spécification")
            return 1
        print("vectors.json à jour")
        return 0

    VECTEURS.write_text(rendu, encoding="utf-8")
    total = sum(len(v) for v in vecteurs.values())
    print(f"{total} vecteurs écrits dans {VECTEURS.relative_to(RACINE)}")
    for nom, liste in vecteurs.items():
        print(f"  {len(liste):3d}  {nom}")

    print("\nAperçu du dimensionnement :")
    for cas in vecteurs["dimensionnement"]:
        a = cas["attendu"]
        detail = ""
        if a["regle_limitante"]:
            lim = next(d for d in a["details"] if d["regle_id"] == a["regle_limitante"])
            detail = (f"  limitante={lim['nom']}  besoin={lim['besoin']:.1f}"
                      f"  brut={lim['nb_brut']:.3f}")
        print(f"  {a['nb_serveurs']:3d} serveurs  {cas['libelle']}{detail}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
