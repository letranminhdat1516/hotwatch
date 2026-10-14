package pages

import "hotwatch/model"

var BuildTree = buildTree

func (d *Docker) LogLen() int                   { return d.logs.len() }
func (d *Docker) LogLineAt(i int) model.LogLine { return d.logs.at(i) }
