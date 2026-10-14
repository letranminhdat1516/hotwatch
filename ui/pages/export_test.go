package pages

import "hotwatch/model"

var BuildTree = buildTree

func (d *Docker) LogLen() int                   { return d.logs.len() }
func (d *Docker) LogLineAt(i int) model.LogLine { return d.logs.at(i) }

func (d *Docker) LogsConnecting() bool {
	d.logs.mu.Lock()
	defer d.logs.mu.Unlock()
	return d.logs.connecting
}
func (d *Docker) LogsEnded() bool { d.logs.mu.Lock(); defer d.logs.mu.Unlock(); return d.logs.ended }

var CleanText = cleanText
