package app

import (
	"pitago/src/components/favorite"
)

// favKey identifies a model option (provider normalized like the left pane).
func (m Model) favKey(prov, id string) string {
	return favorite.Key(normProv(prov), id)
}

// isFavOpt reports whether option ri is starred (render path).
func (m Model) isFavOpt(d *Dialog, ri int) bool {
	if len(m.favSet) == 0 {
		return false
	}
	return m.favSet[m.favKey(providerAt(d.Providers, ri), d.Options[ri])]
}

// toggleFav stars/unstars option ri, persists, and syncs the open dialog.
// Returns true when the model is now starred.
func (m *Model) toggleFav(d *Dialog, ri int) bool {
	prov := providerAt(d.Providers, ri)
	id := d.Options[ri]
	label := ""
	if ri < len(d.Descs) {
		label = d.Descs[ri]
	}
	var now bool
	m.favModels, now = favorite.Toggle(m.favModels, normProv(prov), id, label)
	m.favSet = favorite.Set(m.favModels)
	favorite.Save(m.favPath, m.favModels)
	d.FavSet = favorite.Set(m.favModels)
	return now
}

// isFavIdx reports whether option i is starred (dialog-level, for Reindex).
func (d *Dialog) isFavIdx(i int) bool {
	if d.Kind != "model" || len(d.FavSet) == 0 {
		return false
	}
	return d.FavSet[favorite.Key(normProv(providerAt(d.Providers, i)), d.Options[i])]
}
