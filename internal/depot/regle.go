package depot

import (
	"database/sql"
	"fmt"
	"strings"

	"parallax/internal/capacity"
)

// Regle produit un besoin dans une métrique, sur le domaine défini par son
// filtre (les six dimensions, toutes facultatives). Deux règles actives d'une
// même métrique ne peuvent pas matcher un même cluster : c'est l'invariant 2
// du §12, vérifié à l'enregistrement.
type Regle struct {
	ID               int64
	Nom              string
	MetriqueID       int64
	Expression       string
	NiveauEvaluation string
	ComposantOffre   *string

	ProjetID        *int64
	EnvironnementID *int64
	TechnoID        *int64
	TierID          *int64
	UsageID         *int64
	ClusterID       *int64

	Actif        bool
	Commentaire  *string
	DateCreation string
}

// ConflitRegle décrit un recouvrement interdit entre deux règles d'une même
// métrique, avec les clusters concernés — de quoi refuser en nommant la règle
// concurrente.
type ConflitRegle struct {
	RegleA, RegleB int64
	NomA, NomB     string
	Clusters       []int64
}

func (c ConflitRegle) Error() string {
	return fmt.Sprintf(
		"la règle « %s » recouvre « %s » sur %d cluster(s) : aucune priorité implicite n'est permise",
		c.NomB, c.NomA, len(c.Clusters))
}

// CreerRegle valide et insère une règle. L'expression est compilée à la
// saisie (syntaxe et fonctions inconnues rejetées ici, pas à l'exécution).
//
// Une règle créée est ACTIVE : c'est le cas courant, et le champ Actif de
// l'entrée est ignoré ici. Pour préparer une règle sans l'activer, la créer
// puis DesactiverRegle, ou passer par DupliquerRegle. Si la règle recouvre une
// autre règle active de la même métrique, l'enregistrement est refusé
// (ErrInvariant) en nommant la concurrente.
func (d *Depot) CreerRegle(r Regle) (Regle, error) {
	if err := validerRegle(r); err != nil {
		return Regle{}, err
	}
	r.DateCreation = Horodatage()
	r.Actif = true

	var id int64
	err := d.enTx(func(tx *sql.Tx) error {
		nouvelID, err := insererRegle(tx, r)
		if err != nil {
			return err
		}
		id = nouvelID
		if err := verifierNonRecouvrement(tx, r.MetriqueID, id); err != nil {
			return err
		}
		return d.journalCreationTable(tx, "regle", "regle", id)
	})
	if err != nil {
		return Regle{}, err
	}
	return d.LireRegle(id)
}

// insererRegle écrit une ligne regle et renvoie son identifiant, sans aucun
// contrôle de recouvrement — l'appelant s'en charge selon le contexte.
func insererRegle(c conn, r Regle) (int64, error) {
	res, err := c.Exec(
		`INSERT INTO regle
		 (nom, metrique_id, expression, niveau_evaluation, composant_offre,
		  projet_id, environnement_id, techno_id, tier_id, usage_fonctionnel_id,
		  cluster_id, actif, commentaire, date_creation)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Nom, r.MetriqueID, r.Expression, r.NiveauEvaluation, r.ComposantOffre,
		r.ProjetID, r.EnvironnementID, r.TechnoID, r.TierID, r.UsageID,
		r.ClusterID, boolInt(r.Actif), r.Commentaire, r.DateCreation)
	if err != nil {
		return 0, traduire("création de la règle "+r.Nom, err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// LireRegle renvoie une règle par identifiant, ou ErrIntrouvable.
func (d *Depot) LireRegle(id int64) (Regle, error) {
	return scanRegle(d.base.QueryRow(sqlSelectRegle+` WHERE id = ?`, id),
		fmt.Sprintf("lecture de la règle %d", id))
}

// ListerRegles renvoie les règles d'une métrique (toutes si metriqueID vaut 0),
// actives et inactives, triées par identifiant.
func (d *Depot) ListerRegles(metriqueID int64) ([]Regle, error) {
	requete := sqlSelectRegle
	var args []any
	if metriqueID != 0 {
		requete += ` WHERE metrique_id = ?`
		args = append(args, metriqueID)
	}
	requete += ` ORDER BY id`

	lignes, err := d.base.Query(requete, args...)
	if err != nil {
		return nil, traduire("liste des règles", err)
	}
	defer lignes.Close()

	var out []Regle
	for lignes.Next() {
		r, err := scanRegleLignes(lignes)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, lignes.Err()
}

// ModifierRegle met à jour une règle existante. Mêmes contrôles qu'à la
// création, y compris le refus de recouvrement si la règle reste active.
func (d *Depot) ModifierRegle(r Regle) error {
	if err := validerRegle(r); err != nil {
		return err
	}
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "regle", r.ID)
		if err != nil {
			return err
		}
		res, err := tx.Exec(
			`UPDATE regle SET nom = ?, metrique_id = ?, expression = ?,
			   niveau_evaluation = ?, composant_offre = ?, projet_id = ?,
			   environnement_id = ?, techno_id = ?, tier_id = ?,
			   usage_fonctionnel_id = ?, cluster_id = ?, actif = ?, commentaire = ?
			 WHERE id = ?`,
			r.Nom, r.MetriqueID, r.Expression, r.NiveauEvaluation, r.ComposantOffre,
			r.ProjetID, r.EnvironnementID, r.TechnoID, r.TierID, r.UsageID,
			r.ClusterID, boolInt(r.Actif), r.Commentaire, r.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification de la règle %d", r.ID), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("modification de la règle %d", r.ID)); err != nil {
			return err
		}
		if r.Actif {
			if err := verifierNonRecouvrement(tx, r.MetriqueID, r.ID); err != nil {
				return err
			}
		}
		return d.journalModificationTable(tx, "regle", "regle", r.ID, ActionModification, avant)
	})
}

// ActiverRegle réactive une règle. L'activation peut créer un recouvrement :
// elle est alors refusée (ErrInvariant).
func (d *Depot) ActiverRegle(id int64) error {
	return d.enTx(func(tx *sql.Tx) error {
		var metriqueID int64
		if err := tx.QueryRow(`SELECT metrique_id FROM regle WHERE id = ?`, id).
			Scan(&metriqueID); err != nil {
			if err == sql.ErrNoRows {
				return fmt.Errorf("activation de la règle %d : %w", id, ErrIntrouvable)
			}
			return err
		}
		avant, err := instantane(tx, "regle", id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE regle SET actif = 1 WHERE id = ?`, id); err != nil {
			return traduire(fmt.Sprintf("activation de la règle %d", id), err)
		}
		if err := verifierNonRecouvrement(tx, metriqueID, id); err != nil {
			return err
		}
		return d.journalModificationTable(tx, "regle", "regle", id, ActionModification, avant)
	})
}

// DesactiverRegle désactive une règle. Toujours permis : c'est ainsi qu'on
// fait évoluer une règle d'une année sur l'autre sans perdre la précédente.
func (d *Depot) DesactiverRegle(id int64) error {
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "regle", id)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE regle SET actif = 0 WHERE id = ?`, id)
		if err != nil {
			return traduire(fmt.Sprintf("désactivation de la règle %d", id), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("désactivation de la règle %d", id)); err != nil {
			return err
		}
		return d.journalModificationTable(tx, "regle", "regle", id, ActionModification, avant)
	})
}

// DupliquerRegle crée une copie inactive d'une règle, à ajuster puis activer.
// La copie est inactive pour ne pas provoquer de recouvrement immédiat : c'est
// ainsi qu'on fait évoluer une règle d'une année sur l'autre sans perdre la
// précédente.
func (d *Depot) DupliquerRegle(id int64) (Regle, error) {
	source, err := d.LireRegle(id)
	if err != nil {
		return Regle{}, err
	}
	source.ID = 0
	source.Nom += " (copie)"
	source.Actif = false
	source.DateCreation = Horodatage()

	var nouvelID int64
	err = d.enTx(func(tx *sql.Tx) error {
		id, err := insererRegle(tx, source)
		if err != nil {
			return err
		}
		nouvelID = id
		return d.journalCreationTable(tx, "regle", "regle", id)
	})
	if err != nil {
		return Regle{}, err
	}
	return d.LireRegle(nouvelID)
}

// ReglesCapaciteActives renvoie les règles actives (toutes métriques si
// metriqueID vaut 0) déjà converties au format attendu par internal/capacity
// (code de métrique résolu par jointure). Sert aux écrans qui évaluent un
// besoin — besoin / offre / écart, dimensionnement — sans avoir à refaire
// cette conversion à la main.
func (d *Depot) ReglesCapaciteActives(metriqueID int64) ([]capacity.Regle, error) {
	return chargerReglesCapacite(d.base, metriqueID, true)
}

// DetecterConflitsRegles liste les recouvrements entre règles actives d'une
// métrique (toutes métriques si metriqueID vaut 0). Sert d'écran de
// diagnostic ; l'enregistrement les refuse déjà un par un.
func (d *Depot) DetecterConflitsRegles(metriqueID int64) ([]ConflitRegle, error) {
	regles, err := chargerReglesCapacite(d.base, metriqueID, true)
	if err != nil {
		return nil, err
	}
	clusters, err := chargerClustersCapacite(d.base)
	if err != nil {
		return nil, err
	}
	noms := map[int64]string{}
	for _, r := range regles {
		noms[r.ID] = r.Nom
	}

	var out []ConflitRegle
	for _, c := range capacity.DetecterConflits(regles, clusters) {
		out = append(out, ConflitRegle{
			RegleA: c.RegleA, RegleB: c.RegleB,
			NomA: noms[c.RegleA], NomB: noms[c.RegleB],
			Clusters: c.Clusters,
		})
	}
	return out, nil
}

// --------------------------------------------------------------- helpers

const sqlSelectRegle = `SELECT id, nom, metrique_id, expression, niveau_evaluation,
	composant_offre, projet_id, environnement_id, techno_id, tier_id,
	usage_fonctionnel_id, cluster_id, actif, commentaire, date_creation FROM regle`

func validerRegle(r Regle) error {
	if strings.TrimSpace(r.Nom) == "" {
		return fmt.Errorf("règle : %w : nom vide", ErrValidation)
	}
	if r.NiveauEvaluation != capacity.NiveauPerimetre && r.NiveauEvaluation != capacity.NiveauParServeur {
		return fmt.Errorf("règle « %s » : %w : niveau d'évaluation « %s » inconnu",
			r.Nom, ErrValidation, r.NiveauEvaluation)
	}
	if _, err := capacity.Compiler(r.Expression); err != nil {
		return fmt.Errorf("règle « %s » : %w : expression invalide : %v", r.Nom, ErrValidation, err)
	}
	return nil
}

// verifierNonRecouvrement échoue avec ErrInvariant si la règle candidate
// recouvre une autre règle active de la même métrique sur un cluster commun.
func verifierNonRecouvrement(tx *sql.Tx, metriqueID, candidateID int64) error {
	regles, err := chargerReglesCapacite(tx, metriqueID, true)
	if err != nil {
		return err
	}
	clusters, err := chargerClustersCapacite(tx)
	if err != nil {
		return err
	}
	noms := map[int64]string{}
	for _, r := range regles {
		noms[r.ID] = r.Nom
	}

	for _, c := range capacity.DetecterConflits(regles, clusters) {
		if c.RegleA != candidateID && c.RegleB != candidateID {
			continue
		}
		autre := c.RegleA
		if autre == candidateID {
			autre = c.RegleB
		}
		return fmt.Errorf(
			"enregistrement de la règle %d : %w : recouvre la règle active « %s » (%d) sur %d cluster(s)",
			candidateID, ErrInvariant, noms[autre], autre, len(c.Clusters))
	}
	return nil
}

func chargerReglesCapacite(c conn, metriqueID int64, actifSeulement bool) ([]capacity.Regle, error) {
	requete := `SELECT r.id, r.nom, m.code, r.expression, r.niveau_evaluation,
	                   COALESCE(r.composant_offre, ''), r.projet_id, r.environnement_id,
	                   r.techno_id, r.tier_id, r.usage_fonctionnel_id, r.cluster_id, r.actif
	            FROM regle r JOIN metrique m ON m.id = r.metrique_id`
	var conds []string
	var args []any
	if metriqueID != 0 {
		conds = append(conds, "r.metrique_id = ?")
		args = append(args, metriqueID)
	}
	if actifSeulement {
		conds = append(conds, "r.actif = 1")
	}
	if len(conds) > 0 {
		requete += " WHERE " + strings.Join(conds, " AND ")
	}

	lignes, err := c.Query(requete, args...)
	if err != nil {
		return nil, traduire("chargement des règles", err)
	}
	defer lignes.Close()

	var out []capacity.Regle
	for lignes.Next() {
		var r capacity.Regle
		if err := lignes.Scan(&r.ID, &r.Nom, &r.Metrique, &r.Expression, &r.NiveauEvaluation,
			&r.ComposantOffre, &r.ProjetID, &r.EnvironnementID, &r.TechnoID, &r.TierID,
			&r.UsageID, &r.ClusterID, &r.Actif); err != nil {
			return nil, fmt.Errorf("chargement des règles : %w", err)
		}
		out = append(out, r)
	}
	return out, lignes.Err()
}

func chargerClustersCapacite(c conn) ([]capacity.Cluster, error) {
	lignes, err := c.Query(
		`SELECT id, projet_id, environnement_id, techno_id, tier_id, usage_fonctionnel_id
		 FROM cluster`)
	if err != nil {
		return nil, traduire("chargement des clusters", err)
	}
	defer lignes.Close()

	var out []capacity.Cluster
	for lignes.Next() {
		var cl capacity.Cluster
		if err := lignes.Scan(&cl.ID, &cl.ProjetID, &cl.EnvironnementID, &cl.TechnoID,
			&cl.TierID, &cl.UsageID); err != nil {
			return nil, fmt.Errorf("chargement des clusters : %w", err)
		}
		out = append(out, cl)
	}
	return out, lignes.Err()
}

func scanRegle(row *sql.Row, contexte string) (Regle, error) {
	var r Regle
	err := scanUn(row, contexte,
		&r.ID, &r.Nom, &r.MetriqueID, &r.Expression, &r.NiveauEvaluation, &r.ComposantOffre,
		&r.ProjetID, &r.EnvironnementID, &r.TechnoID, &r.TierID, &r.UsageID, &r.ClusterID,
		&r.Actif, &r.Commentaire, &r.DateCreation)
	return r, err
}

func scanRegleLignes(l *sql.Rows) (Regle, error) {
	var r Regle
	err := l.Scan(
		&r.ID, &r.Nom, &r.MetriqueID, &r.Expression, &r.NiveauEvaluation, &r.ComposantOffre,
		&r.ProjetID, &r.EnvironnementID, &r.TechnoID, &r.TierID, &r.UsageID, &r.ClusterID,
		&r.Actif, &r.Commentaire, &r.DateCreation)
	if err != nil {
		return Regle{}, fmt.Errorf("lecture d'une règle : %w", err)
	}
	return r, nil
}
