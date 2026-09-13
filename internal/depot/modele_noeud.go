package depot

import (
	"database/sql"
	"errors"
	"fmt"
)

// ModeleNoeud dit combien de nœuds d'une technologie donnée s'installent sur
// une révision de modèle : six nœuds Elasticsearch ou Kafka sur une machine
// donnée, mais parfois un seul nœud (bases analytiques). C'est donc une table
// (révision × techno → nombre), pas une valeur unique ; elle sert aussi au
// calcul des licences.
type ModeleNoeud struct {
	ID         int64
	RevisionID int64
	TechnoID   int64
	NbNoeuds   int64
}

// DefinirNoeuds fixe (ou met à jour) le nombre de nœuds installables d'une
// technologie sur une révision. C'est un upsert sur la clé (revision_id,
// techno_id).
//
// Invariant 1 : si la révision est référencée par un serveur_revision et que
// correction vaut false, l'opération est refusée avec ErrImmuable.
//
// Erreurs : ErrValidation si nbNoeuds < 0 ; ErrReference si la révision ou la
// techno n'existe pas ; ErrImmuable comme décrit ci-dessus.
func (d *Depot) DefinirNoeuds(revisionID, technoID int64, nbNoeuds int, correction bool) error {
	if nbNoeuds < 0 {
		return fmt.Errorf("définition des nœuds de la révision %d : %w : nombre « %d » négatif",
			revisionID, ErrValidation, nbNoeuds)
	}

	return d.enTx(func(tx *sql.Tx) error {
		if err := garantirRevisionModifiable(tx, revisionID, correction); err != nil {
			return err
		}
		// journal : les nœuds sont une propriété de la révision (clé
		// composite) — l'entrée porte l'identifiant de la révision, avec
		// la techno et les valeurs avant/après dans les états.
		var avant map[string]any
		var ancien int
		switch err := tx.QueryRow(`SELECT nb_noeuds FROM modele_noeud WHERE revision_id = ? AND techno_id = ?`,
			revisionID, technoID).Scan(&ancien); {
		case err == nil:
			avant = map[string]any{"revision_id": revisionID, "techno_id": technoID, "nb_noeuds": ancien}
		case errors.Is(err, sql.ErrNoRows):
		default:
			return fmt.Errorf("lecture des nœuds de la révision %d : %w", revisionID, err)
		}
		_, err := tx.Exec(
			`INSERT INTO modele_noeud (revision_id, techno_id, nb_noeuds)
			 VALUES (?, ?, ?)
			 ON CONFLICT (revision_id, techno_id)
			 DO UPDATE SET nb_noeuds = excluded.nb_noeuds`,
			revisionID, technoID, nbNoeuds)
		if err != nil {
			return traduire(
				fmt.Sprintf("définition des nœuds de la révision %d (techno %d)", revisionID, technoID),
				err)
		}
		action := actionCatalogue(correction, ActionModification)
		if avant == nil && !correction {
			action = ActionCreation
		}
		return d.journaliser(tx, "modele_noeud", revisionID, action, avant,
			map[string]any{"revision_id": revisionID, "techno_id": technoID, "nb_noeuds": nbNoeuds})
	})
}

// LireNoeuds renvoie le nombre de nœuds installables d'une technologie sur une
// révision, ou ErrIntrouvable si le couple n'a pas été défini.
func (d *Depot) LireNoeuds(revisionID, technoID int64) (int, error) {
	var n int
	err := scanUn(
		d.base.QueryRow(
			`SELECT nb_noeuds FROM modele_noeud WHERE revision_id = ? AND techno_id = ?`,
			revisionID, technoID),
		fmt.Sprintf("lecture des nœuds de la révision %d (techno %d)", revisionID, technoID),
		&n)
	return n, err
}

// ListerNoeuds renvoie toutes les définitions de nœuds d'une révision, triées
// par technologie.
func (d *Depot) ListerNoeuds(revisionID int64) ([]ModeleNoeud, error) {
	lignes, err := d.base.Query(
		`SELECT id, revision_id, techno_id, nb_noeuds
		 FROM modele_noeud WHERE revision_id = ? ORDER BY techno_id`, revisionID)
	if err != nil {
		return nil, traduire(fmt.Sprintf("liste des nœuds de la révision %d", revisionID), err)
	}
	defer lignes.Close()

	var out []ModeleNoeud
	for lignes.Next() {
		var m ModeleNoeud
		if err := lignes.Scan(&m.ID, &m.RevisionID, &m.TechnoID, &m.NbNoeuds); err != nil {
			return nil, fmt.Errorf("liste des nœuds de la révision %d : %w", revisionID, err)
		}
		out = append(out, m)
	}
	return out, lignes.Err()
}
