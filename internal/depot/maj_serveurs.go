package depot

import (
	"database/sql"
	"fmt"
)

// Mise à jour en masse des serveurs (backlog v3.5) : la couche web construit
// un plan — une MajServeur par ligne du fichier, seulement pour ce qui
// change — et le dépôt l'applique dans une transaction unique. Une ligne
// qui échoue à l'écriture (chevauchement découvert au dernier moment,
// référence disparue depuis l'analyse) annule tout : jamais d'import
// partiel, comme pour les créations. Chaque geste passe par le helper
// transactionnel du geste manuel correspondant, donc est journalisé de la
// même façon.

// MajServeur est le plan de mise à jour d'un serveur. Un champ nil ne fait
// rien. L'ordre d'application est fixe : attributs, statut, rattachement de
// révision, affectation.
type MajServeur struct {
	ServeurID    int64
	Attributs    *Serveur         // la ligne complète à écrire (déjà fusionnée avec l'existant)
	Statut       *string          // COMMANDE | EN_SERVICE | DECOMMISSIONNE
	Rattachement *MajRattachement // nouvelle révision à rattacher
	Affectation  *MajAffectation  // nouveau cluster, dans le réel
}

// MajRattachement : rattacher revisionID à partir de Date (l'ouvert
// éventuel est clos la veille par rattacherRevisionTx).
type MajRattachement struct {
	RevisionID int64
	Date       string
}

// MajAffectation : affecter au cluster à partir de Date. Reaffecter vaut
// vrai quand une affectation réelle est active (elle sera close la veille),
// faux pour une simple ouverture.
type MajAffectation struct {
	ClusterID  int64
	Date       string
	Reaffecter bool
}

// Vide indique un plan sans aucun geste.
func (m MajServeur) Vide() bool {
	return m.Attributs == nil && m.Statut == nil && m.Rattachement == nil && m.Affectation == nil
}

// AppliquerMajServeurs applique tous les plans dans une transaction et
// renvoie le nombre de serveurs effectivement modifiés (plans non vides).
//
// Erreurs : celles des gestes sous-jacents (ErrValidation, ErrChevauchement,
// ErrReference, ErrIntrouvable), enrichies de l'identifiant du serveur ;
// la transaction est alors entièrement annulée.
func (d *Depot) AppliquerMajServeurs(plans []MajServeur) (int, error) {
	for _, p := range plans {
		if p.Statut != nil && !statutValide(*p.Statut) {
			return 0, fmt.Errorf("serveur %d : %w : statut « %s » inconnu", p.ServeurID, ErrValidation, *p.Statut)
		}
		if p.Attributs != nil && !TypologieValide(p.Attributs.Typologie) {
			return 0, erreurTypologie(fmt.Sprintf("serveur %d", p.ServeurID), p.Attributs.Typologie)
		}
		if p.Rattachement != nil {
			if err := ValiderDate("date_rattachement", p.Rattachement.Date); err != nil {
				return 0, fmt.Errorf("serveur %d : %w", p.ServeurID, err)
			}
		}
		if p.Affectation != nil {
			if err := ValiderDate("date_affectation", p.Affectation.Date); err != nil {
				return 0, fmt.Errorf("serveur %d : %w", p.ServeurID, err)
			}
		}
	}

	n := 0
	err := d.enTx(func(tx *sql.Tx) error {
		for _, p := range plans {
			if p.Vide() {
				continue
			}
			if p.Attributs != nil {
				a := *p.Attributs
				a.ID = p.ServeurID
				if err := d.modifierServeurTx(tx, a); err != nil {
					return fmt.Errorf("serveur %d : %w", p.ServeurID, err)
				}
			}
			if p.Statut != nil {
				if err := d.changerStatutTx(tx, p.ServeurID, *p.Statut); err != nil {
					return fmt.Errorf("serveur %d : %w", p.ServeurID, err)
				}
			}
			if p.Rattachement != nil {
				if err := d.rattacherRevisionTx(tx, p.ServeurID, p.Rattachement.RevisionID, p.Rattachement.Date, nil); err != nil {
					return fmt.Errorf("serveur %d : %w", p.ServeurID, err)
				}
			}
			if p.Affectation != nil {
				var err error
				if p.Affectation.Reaffecter {
					err = d.reaffecterTx(tx, p.ServeurID, p.Affectation.ClusterID, p.Affectation.Date, nil, nil)
				} else {
					err = d.affecterTx(tx, p.ServeurID, p.Affectation.ClusterID, p.Affectation.Date, nil, nil)
				}
				if err != nil {
					return fmt.Errorf("serveur %d : %w", p.ServeurID, err)
				}
			}
			n++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ServeursParCle résout les serveurs réels portant une valeur de clé
// (hostname, physical_name) — plusieurs résultats signalent une ambiguïté
// que l'import doit refuser. Les serveurs hypothétiques sont exclus : la
// mise à jour en masse ne vise que le réel.
func (d *Depot) ServeursParCle(colonne, valeur string) ([]int64, error) {
	switch colonne {
	case "hostname", "physical_name":
	default:
		return nil, fmt.Errorf("clé de rapprochement « %s » : %w", colonne, ErrValidation)
	}
	return identifiants(d.base,
		`SELECT id FROM serveur WHERE scenario_id IS NULL AND `+colonne+` = ? ORDER BY id`, valeur)
}

// ServeursParDemande résout les serveurs réels par le couple numéro de
// demande + référence de fiche.
func (d *Depot) ServeursParDemande(demandeRef, serveurRef string) ([]int64, error) {
	return identifiants(d.base,
		`SELECT id FROM serveur WHERE scenario_id IS NULL AND demande_ref = ? AND demande_serveur_ref = ? ORDER BY id`,
		demandeRef, serveurRef)
}
