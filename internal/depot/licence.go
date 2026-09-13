package depot

import (
	"database/sql"
	"fmt"
	"strings"
)

// Mécanismes de comptage d'un contrat de licence (docs/modele-donnees.md
// §8.2) : ce que le contrat compte, sur le périmètre choisi dans une vue.
const (
	MecanismeLicenceNoeuds       = "NOEUDS"         // Σ nb_noeuds
	MecanismeLicenceMaxNoeudsRam = "MAX_NOEUDS_RAM" // max(nb_noeuds, ceil(ram / ram_max))
	MecanismeLicenceRam          = "RAM"            // ceil(ram / ram_max)
)

// Niveaux d'application du mécanisme : l'échelle à laquelle la formule est
// évaluée avant d'être sommée. Sans objet pour NOEUDS (une somme est une
// somme), mais toujours renseigné pour garder une ligne homogène.
const (
	NiveauLicenceMachine = "MACHINE" // par serveur, puis somme
	NiveauLicenceCluster = "CLUSTER" // par cluster, puis somme
	NiveauLicenceGlobal  = "GLOBAL"  // sur tout le groupe de la vue — non additif
)

var mecanismesLicenceValides = map[string]bool{
	MecanismeLicenceNoeuds: true, MecanismeLicenceMaxNoeudsRam: true, MecanismeLicenceRam: true,
}

var niveauxLicenceValides = map[string]bool{
	NiveauLicenceMachine: true, NiveauLicenceCluster: true, NiveauLicenceGlobal: true,
}

// LicenceContrat est le contrat de licence d'une techno pour une année,
// surchargeable par scénario (v3.3). Un contrat court jusqu'au suivant : la
// résolution pour une année prend le contrat de cette année, à défaut le
// plus récent des années antérieures (ResoudreContrats).
type LicenceContrat struct {
	ID             int64
	TechnoID       int64
	Annee          int
	ScenarioID     *int64 // nil = réel
	Mecanisme      string // NOEUDS | MAX_NOEUDS_RAM | RAM
	Niveau         string // MACHINE | CLUSTER | GLOBAL
	RamMaxGo       *float64
	CoutUnitaireHT *float64
	Commentaire    *string
}

// validerLicenceContrat normalise et vérifie un contrat avant écriture.
// ram_max_go est requis et strictement positif sauf pour NOEUDS, où il est
// sans objet et remis à nil — un contrat qui repasse en nœuds ne traîne pas
// une RAM maximale fantôme.
func validerLicenceContrat(c *LicenceContrat) error {
	c.Mecanisme = strings.ToUpper(strings.TrimSpace(c.Mecanisme))
	c.Niveau = strings.ToUpper(strings.TrimSpace(c.Niveau))
	if c.TechnoID <= 0 {
		return fmt.Errorf("contrat de licence : %w : techno obligatoire", ErrValidation)
	}
	if c.Annee < 2000 || c.Annee > 2100 {
		return fmt.Errorf("contrat de licence : %w : année %d hors plage", ErrValidation, c.Annee)
	}
	if !mecanismesLicenceValides[c.Mecanisme] {
		return fmt.Errorf("contrat de licence : %w : mécanisme « %s » inconnu (NOEUDS, MAX_NOEUDS_RAM ou RAM)",
			ErrValidation, c.Mecanisme)
	}
	if !niveauxLicenceValides[c.Niveau] {
		return fmt.Errorf("contrat de licence : %w : niveau « %s » inconnu (MACHINE, CLUSTER ou GLOBAL)",
			ErrValidation, c.Niveau)
	}
	if c.Mecanisme == MecanismeLicenceNoeuds {
		c.RamMaxGo = nil
	} else if c.RamMaxGo == nil || *c.RamMaxGo <= 0 {
		return fmt.Errorf("contrat de licence : %w : RAM max (Go) obligatoire et strictement positive pour le mécanisme %s",
			ErrValidation, c.Mecanisme)
	}
	if c.CoutUnitaireHT != nil && *c.CoutUnitaireHT < 0 {
		return fmt.Errorf("contrat de licence : %w : coût unitaire négatif", ErrValidation)
	}
	if c.Commentaire != nil {
		v := strings.TrimSpace(*c.Commentaire)
		if v == "" {
			c.Commentaire = nil
		} else {
			c.Commentaire = &v
		}
	}
	return nil
}

// CreerLicenceContrat insère un contrat et renvoie la ligne complète.
//
// Erreurs : ErrValidation (voir validerLicenceContrat) ; ErrConflit si un
// contrat existe déjà pour cette techno, cette année et ce seau (réel ou
// scénario) ; ErrReference si la techno ou le scénario n'existe pas.
func (d *Depot) CreerLicenceContrat(c LicenceContrat) (LicenceContrat, error) {
	if err := validerLicenceContrat(&c); err != nil {
		return LicenceContrat{}, err
	}
	err := d.enTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`INSERT INTO licence_contrat
			 (techno_id, annee, scenario_id, mecanisme, niveau, ram_max_go, cout_unitaire_ht, commentaire)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			c.TechnoID, c.Annee, c.ScenarioID, c.Mecanisme, c.Niveau, c.RamMaxGo, c.CoutUnitaireHT, c.Commentaire)
		if err != nil {
			return traduire("création du contrat de licence", err)
		}
		c.ID, _ = res.LastInsertId()
		return d.journalCreationTable(tx, "licence_contrat", "licence_contrat", c.ID)
	})
	if err != nil {
		return LicenceContrat{}, err
	}
	return c, nil
}

// LireLicenceContrat renvoie un contrat par identifiant, ou ErrIntrouvable.
func (d *Depot) LireLicenceContrat(id int64) (LicenceContrat, error) {
	var c LicenceContrat
	err := scanUn(
		d.base.QueryRow(
			`SELECT id, techno_id, annee, scenario_id, mecanisme, niveau, ram_max_go, cout_unitaire_ht, commentaire
			 FROM licence_contrat WHERE id = ?`, id),
		fmt.Sprintf("lecture du contrat de licence %d", id),
		&c.ID, &c.TechnoID, &c.Annee, &c.ScenarioID, &c.Mecanisme, &c.Niveau,
		&c.RamMaxGo, &c.CoutUnitaireHT, &c.Commentaire)
	return c, err
}

// ListerLicenceContrats renvoie les contrats bruts : ceux du réel et, si
// scenarioID est non nil, les surcharges de ce scénario — ensemble, pour
// que l'écran les montre côte à côte (même principe que ListerContraintes).
// Triés par techno, année, puis réel avant scénario.
func (d *Depot) ListerLicenceContrats(scenarioID *int64) ([]LicenceContrat, error) {
	requete := `SELECT lc.id, lc.techno_id, lc.annee, lc.scenario_id, lc.mecanisme, lc.niveau,
	                   lc.ram_max_go, lc.cout_unitaire_ht, lc.commentaire
	            FROM licence_contrat lc
	            JOIN techno t ON t.id = lc.techno_id
	            WHERE (lc.scenario_id IS NULL`
	var args []any
	if scenarioID != nil {
		requete += ` OR lc.scenario_id = ?`
		args = append(args, *scenarioID)
	}
	requete += `) ORDER BY t.code, lc.annee, (lc.scenario_id IS NOT NULL), lc.id`

	return listerLicenceContrats(d.base, requete, args...)
}

func listerLicenceContrats(c conn, requete string, args ...any) ([]LicenceContrat, error) {
	lignes, err := c.Query(requete, args...)
	if err != nil {
		return nil, traduire("liste des contrats de licence", err)
	}
	defer lignes.Close()

	var out []LicenceContrat
	for lignes.Next() {
		var lc LicenceContrat
		if err := lignes.Scan(&lc.ID, &lc.TechnoID, &lc.Annee, &lc.ScenarioID, &lc.Mecanisme, &lc.Niveau,
			&lc.RamMaxGo, &lc.CoutUnitaireHT, &lc.Commentaire); err != nil {
			return nil, fmt.Errorf("liste des contrats de licence : %w", err)
		}
		out = append(out, lc)
	}
	return out, lignes.Err()
}

// ModifierLicenceContrat met à jour techno, année, mécanisme, niveau, RAM
// max, coût et commentaire. Le seau (ScenarioID) ne change jamais : un
// contrat écrit dans un scénario y reste, comme une valeur de variable.
//
// Erreurs : ErrIntrouvable ; ErrValidation ; ErrConflit si (techno, année)
// entre en collision avec un autre contrat du même seau.
func (d *Depot) ModifierLicenceContrat(c LicenceContrat) error {
	if err := validerLicenceContrat(&c); err != nil {
		return err
	}
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "licence_contrat", c.ID)
		if err != nil {
			return err
		}
		res, err := tx.Exec(
			`UPDATE licence_contrat
			 SET techno_id = ?, annee = ?, mecanisme = ?, niveau = ?, ram_max_go = ?,
			     cout_unitaire_ht = ?, commentaire = ?
			 WHERE id = ?`,
			c.TechnoID, c.Annee, c.Mecanisme, c.Niveau, c.RamMaxGo, c.CoutUnitaireHT, c.Commentaire, c.ID)
		if err != nil {
			return traduire(fmt.Sprintf("modification du contrat de licence %d", c.ID), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("modification du contrat de licence %d", c.ID)); err != nil {
			return err
		}
		return d.journalModificationTable(tx, "licence_contrat", "licence_contrat", c.ID, ActionModification, avant)
	})
}

// SupprimerLicenceContrat retire un contrat. Comme une contrainte, c'est un
// paramètre de calcul indexé par (scénario, année) : le retirer est une
// édition normale, pas une destruction de donnée du parc.
func (d *Depot) SupprimerLicenceContrat(id int64) error {
	return d.enTx(func(tx *sql.Tx) error {
		avant, err := instantane(tx, "licence_contrat", id)
		if err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM licence_contrat WHERE id = ?`, id)
		if err != nil {
			return traduire(fmt.Sprintf("suppression du contrat de licence %d", id), err)
		}
		if err := exigerUneLigne(res, fmt.Sprintf("suppression du contrat de licence %d", id)); err != nil {
			return err
		}
		return d.journalSuppressionTable(tx, "licence_contrat", id, avant)
	})
}

// ResoudreContrats renvoie, par identifiant de techno, le contrat en vigueur
// pour une année sous un scénario (nil = réel).
//
// Règle, dans cet ordre : la surcharge du scénario l'emporte sur le réel ;
// dans chaque seau, on prend le contrat de l'année demandée, à défaut le
// plus récent des années antérieures — un contrat court jusqu'au suivant
// (docs/modele-donnees.md §8.2). Le seau du scénario est résolu en entier
// avant le réel : si le scénario porte un contrat 2025 et le réel un
// contrat 2026, une lecture en 2026 sous ce scénario prend le 2025 du
// scénario — c'est « scénario avant réel », comme pour les variables. Une
// techno sans contrat est absente de la carte.
func (d *Depot) ResoudreContrats(annee int, scenarioID *int64) (map[int64]LicenceContrat, error) {
	requete := `SELECT id, techno_id, annee, scenario_id, mecanisme, niveau,
	                   ram_max_go, cout_unitaire_ht, commentaire
	            FROM licence_contrat
	            WHERE annee <= ? AND (scenario_id IS NULL`
	args := []any{annee}
	if scenarioID != nil {
		requete += ` OR scenario_id = ?`
		args = append(args, *scenarioID)
	}
	// réel avant scénario, puis années croissantes : la dernière écriture par
	// techno est la bonne (année la plus récente du seau le plus prioritaire).
	requete += `) ORDER BY (scenario_id IS NOT NULL), annee`

	contrats, err := listerLicenceContrats(d.base, requete, args...)
	if err != nil {
		return nil, err
	}
	out := map[int64]LicenceContrat{}
	for _, c := range contrats {
		if existant, ok := out[c.TechnoID]; ok && existant.ScenarioID != nil && c.ScenarioID == nil {
			continue // ne peut pas arriver avec ce tri, gardé par sûreté
		}
		out[c.TechnoID] = c
	}
	return out, nil
}
