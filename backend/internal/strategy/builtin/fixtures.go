package builtin

import "github.com/hyperagent/hyperagent/internal/strategy/venue"

// FixtureMarkets is the reference tape the builtin tests, the API tests and
// the CLI's offline dry run share: a crowded-long ETH (funding +8 bps/h,
// premium up, price up), a quiet BTC, SOL and HYPE down on the day.
func FixtureMarkets() []venue.Market {
	return []venue.Market{
		{Symbol: "BTC", Mark: 60000, Mid: 60005, Funding: 0.00001, Premium: 0.0001, OpenInterestUSD: 3e9, DayVolumeUSD: 2e10, DayChange: 0.004},
		{Symbol: "ETH", Mark: 3000, Mid: 3000.5, Funding: 0.0008, Premium: 0.003, OpenInterestUSD: 1.2e9, DayVolumeUSD: 9e9, DayChange: 0.052},
		{Symbol: "SOL", Mark: 150, Mid: 150, Funding: -0.0001, Premium: -0.0002, OpenInterestUSD: 4e8, DayVolumeUSD: 2e9, DayChange: -0.02},
		{Symbol: "HYPE", Mark: 30, Mid: 30, Funding: 0, Premium: 0, OpenInterestUSD: 2e8, DayVolumeUSD: 5e8, DayChange: -0.015},
	}
}
