package depot

import (
	"database/sql"
	"path/filepath"
	"testing"

	"parallax/internal/db"
)

// baseDeTest ouvre une base SQLite neuve dans un fichier temporaire, migrations
// appliquées. Le fichier (plutôt que :memory:) permet au pool d'ouvrir
// plusieurs connexions sur la même base, comme en production.
func baseDeTest(t *testing.T) *sql.DB {
	t.Helper()
	chemin := filepath.Join(t.TempDir(), "parallax-test.db")
	base, err := db.Ouvrir(chemin)
	if err != nil {
		t.Fatalf("ouverture de la base de test : %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })
	return base
}

// depotDeTest renvoie un Depot prêt à l'emploi sur une base neuve.
func depotDeTest(t *testing.T) *Depot {
	t.Helper()
	return Nouveau(baseDeTest(t))
}

// exec exécute un ordre SQL de préparation de fixture et échoue le test en cas
// d'erreur.
func exec(t *testing.T, c conn, requete string, args ...any) {
	t.Helper()
	if _, err := c.Exec(requete, args...); err != nil {
		t.Fatalf("fixture : %s\n%v", requete, err)
	}
}

// refs regroupe les identifiants des référentiels de base, semés par
// semerRefs. La plupart des tests de cluster, serveur et affectation en ont
// besoin.
type refs struct {
	Projet, Projet2            int64
	EnvProd, EnvQual           int64
	TechnoElastic, TechnoKafka int64
	TierHot, TierCold          int64
	UsageLog                   int64
	DC1, DC2                   int64
}

// semerRefs insère un jeu minimal et stable de référentiels par SQL direct,
// sans passer par les dépôts, pour que les tests d'une entité ne dépendent pas
// de l'implémentation d'une autre.
func semerRefs(t *testing.T, c conn) refs {
	t.Helper()
	exec(t, c, `INSERT INTO projet (id, code, libelle) VALUES
		(1,'LOGS','Log Management Production'),
		(2,'STREAM','Stream')`)
	exec(t, c, `INSERT INTO environnement (id, code, libelle, ordre) VALUES
		(1,'PROD','Production',10),
		(2,'QUAL','Qualification',20)`)
	exec(t, c, `INSERT INTO techno (id, code, libelle) VALUES
		(1,'ELASTIC','Elasticsearch'),
		(2,'KAFKA','Kafka')`)
	exec(t, c, `INSERT INTO tier (id, code, libelle, ordre) VALUES
		(1,'HOT','Hot',10),
		(2,'COLD','Cold',30)`)
	exec(t, c, `INSERT INTO usage_fonctionnel (id, code, libelle) VALUES
		(1,'LOGMGMT','Log Management')`)
	exec(t, c, `INSERT INTO zone (id, code, libelle) VALUES
		(1,'DC1','Zone 1'),
		(2,'DC2','Zone 2')`)
	return refs{
		Projet: 1, Projet2: 2,
		EnvProd: 1, EnvQual: 2,
		TechnoElastic: 1, TechnoKafka: 2,
		TierHot: 1, TierCold: 2,
		UsageLog: 1,
		DC1:      1, DC2: 2,
	}
}

// clusterDeTest insère un cluster minimal (LOGS/PROD/ELASTIC) et renvoie son
// id. Pratique pour les tests de serveur et d'affectation.
func clusterDeTest(t *testing.T, c conn, r refs, nom string) int64 {
	t.Helper()
	res, err := c.Exec(
		`INSERT INTO cluster (nom, projet_id, environnement_id, techno_id)
		 VALUES (?,?,?,?)`,
		nom, r.Projet, r.EnvProd, r.TechnoElastic)
	if err != nil {
		t.Fatalf("fixture cluster : %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}
