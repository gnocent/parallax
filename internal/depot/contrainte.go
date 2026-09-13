package depot

import (
	"database/sql"
	"fmt"

	"parallax/internal/capacity"
)

// Portées d'application de la contrainte EQUILIBRAGE_ZONE.
const (
	PorteeContrainteNouveaux = "NOUVEAUX"
	PorteeContrainteTous     = "TOUS"
)

// typesContrainteValides reprend l'énumération du schéma. Les codes eux-mêmes
// sont définis dans internal/capacity (ContrainteMinTotal…) et partagés.
var typesContrainteValides = map[string]bool{
	capacity.ContrainteMinTotal:        true,
	capacity.ContrainteMinParZone:      true,
	capacity.ContrainteMultipleTotal:   true,
	capacity.ContrainteMultipleParZone: true,
	capacity.ContrainteNbZones:         true,
	capacity.ContrainteEquilibrageZone: true,
}

// Contrainte est une propriété de dimensionnement portée par un cluster,
// indexée par (scénario, année) comme les variables : le nombre de
// zones d'un cluster peut changer d'une année à l'autre, et c'est le
// genre d'hypothèse à comparer.
type Contrainte struct {
	ID          int64
	ClusterID   int64
	ScenarioID  *int64
	Annee       int
	Type        string
	Valeur      *float64
	Portee      *string
	Commentaire *string
}

// DefinirContrainte pose ou remplace une contrainte pour un cluster, une
// année, un type et un scénario donnés (le réel si scenarioID est nil).
//
// Erreurs : ErrValidation si le type est hors énumération ou si EQUILIBRAGE_ZONE
// reçoit une portée absente/invalide ; ErrReference si le cluster n'existe pas.
func (d *Depot) DefinirContrainte(clusterID int64, scenarioID *int64, annee int,
	typ string, valeur *float64, portee *string, commentaire *string) (Contrainte, error) {

	if !typesContrainteValides[typ] {
		return Contrainte{}, fmt.Errorf(
			"contrainte : %w : type « %s » inconnu", ErrValidation, typ)
	}
	if typ == capacity.ContrainteEquilibrageZone {
		if portee == nil || (*portee != PorteeContrainteNouveaux && *portee != PorteeContrainteTous) {
			return Contrainte{}, fmt.Errorf(
				"contrainte EQUILIBRAGE_ZONE : %w : portée NOUVEAUX ou TOUS obligatoire", ErrValidation)
		}
	} else {
		portee = nil // les autres types ne portent pas de portée
	}
	if annee < 2000 || annee > 2100 {
		return Contrainte{}, fmt.Errorf("contrainte : %w : année %d hors plage", ErrValidation, annee)
	}

	var id int64
	err := d.enTx(func(tx *sql.Tx) error {
		var existe int64
		err := tx.QueryRow(
			`SELECT id FROM contrainte
			 WHERE cluster_id = ? AND annee = ? AND type = ?
			   AND COALESCE(scenario_id, -1) = COALESCE(?, -1)`,
			clusterID, annee, typ, scenarioID).Scan(&existe)
		switch err {
		case nil:
			avant, err := instantane(tx, "contrainte", existe)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(
				`UPDATE contrainte SET valeur = ?, portee = ?, commentaire = ? WHERE id = ?`,
				valeur, portee, commentaire, existe); err != nil {
				return traduire("mise à jour de la contrainte", err)
			}
			id = existe
			return d.journalModificationTable(tx, "contrainte", "contrainte", id, ActionModification, avant)
		case sql.ErrNoRows:
			res, err := tx.Exec(
				`INSERT INTO contrainte
				 (cluster_id, scenario_id, annee, type, valeur, portee, commentaire)
				 VALUES (?, ?, ?, ?, ?, ?, ?)`,
				clusterID, scenarioID, annee, typ, valeur, portee, commentaire)
			if err != nil {
				return traduire("création de la contrainte", err)
			}
			id, _ = res.LastInsertId()
			return d.journalCreationTable(tx, "contrainte", "contrainte", id)
		default:
			return fmt.Errorf("contrainte : %w", err)
		}
	})
	if err != nil {
		return Contrainte{}, err
	}
	return d.LireContrainte(id)
}

// LireContrainte renvoie une contrainte par identifiant, ou ErrIntrouvable.
func (d *Depot) LireContrainte(id int64) (Contrainte, error) {
	var c Contrainte
	err := scanUn(
		d.base.QueryRow(
			`SELECT id, cluster_id, scenario_id, annee, type, valeur, portee, commentaire
			 FROM contrainte WHERE id = ?`, id),
		fmt.Sprintf("lecture de la contrainte %d", id),
		&c.ID, &c.ClusterID, &c.ScenarioID, &c.Annee, &c.Type, &c.Valeur, &c.Portee, &c.Commentaire)
	return c, err
}

// ListerContraintes renvoie les contraintes brutes d'un cluster pour une
// année : le réel, et si scenarioID est non nil les surcharges de ce scénario.
// Pour la vue effective, utiliser ContraintesResolues.
func (d *Depot) ListerContraintes(clusterID int64, annee int, scenarioID *int64) ([]Contrainte, error) {
	requete := `SELECT id, cluster_id, scenario_id, annee, type, valeur, portee, commentaire
	            FROM contrainte WHERE cluster_id = ? AND annee = ? AND (scenario_id IS NULL`
	args := []any{clusterID, annee}
	if scenarioID != nil {
		requete += ` OR scenario_id = ?`
		args = append(args, *scenarioID)
	}
	requete += `) ORDER BY type, (scenario_id IS NOT NULL)`

	lignes, err := d.base.Query(requete, args...)
	if err != nil {
		return nil, traduire("liste des contraintes", err)
	}
	defer lignes.Close()

	var out []Contrainte
	for lignes.Next() {
		var c Contrainte
		if err := lignes.Scan(&c.ID, &c.ClusterID, &c.ScenarioID, &c.Annee, &c.Type,
			&c.Valeur, &c.Portee, &c.Commentaire); err != nil {
			return nil, fmt.Errorf("liste des contraintes : %w", err)
		}
		out = append(out, c)
	}
	return out, lignes.Err()
}

// ContraintesResolues construit la carte attendue par le moteur de
// dimensionnement : pour chaque type, la surcharge du scénario l'emporte sur
// le réel. EQUILIBRAGE_ZONE est un drapeau — toute valeur non nulle l'active ;
// on retient 1 quand une contrainte de ce type existe.
func (d *Depot) ContraintesResolues(clusterID int64, annee int, scenarioID *int64) (capacity.Contraintes, error) {
	brutes, err := d.ListerContraintes(clusterID, annee, scenarioID)
	if err != nil {
		return nil, err
	}

	// brutes est triée réel puis scénario : la seconde écriture écrase.
	out := capacity.Contraintes{}
	for _, c := range brutes {
		switch {
		case c.Type == capacity.ContrainteEquilibrageZone:
			out[c.Type] = 1
		case c.Valeur != nil:
			out[c.Type] = *c.Valeur
		}
	}
	return out, nil
}

// SupprimerContrainte retire une contrainte. Une contrainte est un paramètre
// de planification indexé par (scénario, année) : la retirer est une édition
// normale, pas une destruction de donnée du réel.
func (d *Depot) SupprimerContrainte(id int64) error {
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "contrainte", id)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM contrainte WHERE id = ?`, id)
		if err != nil {
			return traduire(fmt.Sprintf("suppression de la contrainte %d", id), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("suppression de la contrainte %d", id)); err != nil {
			return err
		}
		return d.journalSuppressionTable(tx, "contrainte", id, avant)
	})
}
