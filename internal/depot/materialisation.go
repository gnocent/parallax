package depot

import (
	"database/sql"
	"fmt"
	"strings"
)

// Materialisation décrit le résultat d'un dimensionnement inverse (v2.3) à
// poser dans un scénario : Nb serveurs hypothétiques rattachés à une révision
// de modèle et affectés au cluster, et les serveurs installés que l'hypothèse
// ne conserve pas, retirés du cluster dans ce même scénario — le delta de
// l'écran besoin/offre les montre aussitôt.
type Materialisation struct {
	ScenarioID int64
	ClusterID  int64
	RevisionID int64
	Nb         int
	DateDebut  string  // date d'entrée et d'affectation des hypothétiques, date de retrait des non conservés
	Prefixe    string  // nom physique : Prefixe + "-" + numéro d'ordre
	Zones      []int64 // répartition circulaire ; vide = sans zone
	Retirer    []int64 // serveurs installés à retirer du cluster, dans le scénario
}

// Materialiser exécute la matérialisation dans une seule transaction : soit
// tout est posé, soit rien (une hypothèse à moitié écrite serait pire que
// pas d'hypothèse). Renvoie les identifiants des serveurs créés.
//
// Erreurs : ErrValidation (date, nombre, scénario clos) ; ErrReference
// (scénario, révision, cluster ou zone inexistante) ; celles de
// RetirerDuScenario pour les retraits.
func (d *Depot) Materialiser(m Materialisation) ([]int64, error) {
	return d.MaterialiserLot([]Materialisation{m})
}

// MaterialiserLot pose plusieurs matérialisations — typiquement une par
// cluster d'un même scénario — dans une seule transaction : un lot à moitié
// posé laisserait le scénario dans un état que personne n'a choisi. Chaque
// élément est validé comme par Materialiser ; le lot doit contenir au moins
// un élément. Renvoie les identifiants de tous les serveurs créés.
func (d *Depot) MaterialiserLot(lot []Materialisation) ([]int64, error) {
	if len(lot) == 0 {
		return nil, fmt.Errorf("matérialisation : %w : lot vide", ErrValidation)
	}
	for _, m := range lot {
		if err := validerMaterialisation(m); err != nil {
			return nil, err
		}
	}
	var crees []int64
	err := d.enTx(func(tx *sql.Tx) error {
		for _, m := range lot {
			ids, err := d.materialiserTx(tx, m)
			if err != nil {
				return err
			}
			crees = append(crees, ids...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return crees, nil
}

func validerMaterialisation(m Materialisation) error {
	if err := ValiderDate("date de début", m.DateDebut); err != nil {
		return err
	}
	if m.Nb < 0 || m.Nb > 1000 {
		return fmt.Errorf("matérialisation : %w : nombre de serveurs %d hors plage", ErrValidation, m.Nb)
	}
	if m.Nb == 0 && len(m.Retirer) == 0 {
		return fmt.Errorf("matérialisation : %w : rien à poser (aucun serveur à ajouter ni à retirer)", ErrValidation)
	}
	return nil
}

func (d *Depot) materialiserTx(tx *sql.Tx, m Materialisation) ([]int64, error) {
	prefixe := strings.TrimSpace(m.Prefixe)
	if prefixe == "" {
		prefixe = "HYP"
	}
	s, err := lireScenario(tx, m.ScenarioID)
	if err != nil {
		return nil, err
	}
	if s.Statut != ScenarioBrouillon && s.Statut != ScenarioActif {
		return nil, fmt.Errorf("matérialisation : %w : le scénario %s est clos (%s)",
			ErrValidation, s.Nom, s.Statut)
	}

	var crees []int64
	for i := 1; i <= m.Nb; i++ {
		var dc *int64
		if len(m.Zones) > 0 {
			id := m.Zones[(i-1)%len(m.Zones)]
			dc = &id
		}
		nom := fmt.Sprintf("%s-%02d", prefixe, i)
		res, err := tx.Exec(
			`INSERT INTO serveur (physical_name, zone_id, statut, scenario_id, date_entree)
			 VALUES (?, ?, 'HYPOTHESE', ?, ?)`,
			nom, dc, m.ScenarioID, m.DateDebut)
		if err != nil {
			return nil, traduire("matérialisation : serveur "+nom, err)
		}
		id, _ := res.LastInsertId()
		if err := d.journalCreationTable(tx, "serveur", "serveur", id); err != nil {
			return nil, err
		}
		resRatt, err := tx.Exec(
			`INSERT INTO serveur_revision (serveur_id, revision_id, date_debut) VALUES (?, ?, ?)`,
			id, m.RevisionID, m.DateDebut)
		if err != nil {
			return nil, traduire("matérialisation : révision de "+nom, err)
		}
		rattID, _ := resRatt.LastInsertId()
		if err := d.journalCreationTable(tx, "serveur_revision", "serveur_revision", rattID); err != nil {
			return nil, err
		}
		if err := d.affecterTx(tx, id, m.ClusterID, m.DateDebut, &m.ScenarioID, nil); err != nil {
			return nil, err
		}
		crees = append(crees, id)
	}

	for _, serveurID := range m.Retirer {
		if err := d.retirerDuScenarioTx(tx, serveurID, m.ScenarioID, m.DateDebut); err != nil {
			return nil, err
		}
	}
	return crees, nil
}
