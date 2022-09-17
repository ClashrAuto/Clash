package constant

import (
	"github.com/ClashrAuto/clash/component/geodata/router"
)

type RuleGeoSite interface {
	GetDomainMatcher() *router.DomainMatcher
}

type RuleGeoIP interface {
	GetIPMatcher() *router.GeoIPMatcher
}

type RuleGroup interface {
	GetRecodeSize() int
}
