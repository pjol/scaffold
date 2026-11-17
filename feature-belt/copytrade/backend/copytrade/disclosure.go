package copytrade

// DisclosureText is the copy-v1 risk disclosure (GET /copytrade/config),
// which a follower accepts before every new copy. Rewrite it for your product
// (what the money is, who runs it) and change DisclosureVersion whenever its
// substance changes.
const DisclosureText = `## Copy trading: what you are agreeing to (copy-v1)

Nothing here is investment advice or a recommendation to trade.

- **You copy positions, not results.** Your copy account holds positions in proportion to the trader's: their
  size × your copy's value ÷ their account value. Results differ from the trader's because of timing, price moves,
  minimum order sizes, fees and your own limits.
- **You can lose your whole allocation.** Leverage magnifies gains and losses, and a copied position can be
  liquidated. Your stop loss and drawdown limit are checked about every 15 seconds and close at market, so the
  final loss can be larger than the limit.
- **Orders can be skipped.** Orders below the market minimum, beyond your limits, more than 2% past the trader's
  fill price, or rejected by the exchange are skipped and listed in your copy's activity. A background check
  corrects drift about once a minute.
- **Fees.** Every copied order pays the normal trading fee. If the trader takes a profit share, it is paid from
  your copy account on gains above your previous high (a high-water mark), at each settlement and when you withdraw
  or stop.
- **Past results don't predict future ones.** Returns and drawdowns describe the past only.
- **You stay in control.** You can pause, change limits, close positions, add or withdraw free funds, or stop at
  any time. Copying stops by itself if the trader stops offering copies or leaves; positions then close or stay
  according to your setting.
- **Your copy is private by default.** The trader sees how many people copy them and a range for each allocation.
  They see your name only if they show copiers when you start, which the start screen tells you.
`
