package web

import (
	"reflect"
	"testing"
)

func TestFusionsVerticales(t *testing.T) {
	cles := [][]string{
		{"Elastic", "Hot", "DC1"},
		{"Elastic", "Hot", "DC2"},
		{"Elastic", "Cold", "DC1"},
		{"Kafka", "", "DC1"},
		{"Kafka", "", "DC1"},
	}
	attendu := [][]int{
		{3, 2, 1},
		{0, 0, 1},
		{0, 1, 1},
		{2, 2, 2},
		{0, 0, 0},
	}
	if got := fusionsVerticales(cles); !reflect.DeepEqual(got, attendu) {
		t.Fatalf("fusions inattendues :\n%v\nattendu :\n%v", got, attendu)
	}
	// une seule colonne, valeurs distinctes : aucune fusion ; vide : rien
	if got := fusionsVerticales([][]string{{"a"}, {"b"}}); !reflect.DeepEqual(got, [][]int{{1}, {1}}) {
		t.Fatalf("sans doublon : %v", got)
	}
	if got := fusionsVerticales(nil); len(got) != 0 {
		t.Fatalf("vide : %v", got)
	}
}
