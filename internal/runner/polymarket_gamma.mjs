import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

const clientModule = pathToFileURL(resolve('build/lib/polymarketClient.js'));
const { polymarketClient } = await import(clientModule.href);

function outcomesList(value) {
  if (typeof value === 'string') {
    try {
      value = JSON.parse(value);
    } catch {
      return [];
    }
  }
  return Array.isArray(value) ? value : [];
}

function marketForFormatter(market) {
  return {
    ...market,
    outcomes: outcomesList(market.outcomes),
    start_date: market.start_date ?? market.startDate,
    end_date: market.end_date ?? market.endDate,
    condition_id: market.condition_id ?? market.conditionId,
    market_maker_address: market.market_maker_address ?? market.marketMakerAddress,
    enable_order_book: market.enable_order_book ?? market.enableOrderBook,
  };
}

// Gamma serializes outcomes as JSON strings in both its market listings
// and the markets nested inside events. The pinned MCP expects arrays.
const getMarkets = polymarketClient.getMarkets.bind(polymarketClient);
polymarketClient.getMarkets = async (...args) => {
  const markets = await getMarkets(...args);
  return markets.map(marketForFormatter);
};

const getEvents = polymarketClient.getEvents.bind(polymarketClient);
polymarketClient.getEvents = async (...args) => {
  const events = await getEvents(...args);
  return events.map(event => ({
    ...event,
    start_date: event.start_date ?? event.startDate,
    end_date: event.end_date ?? event.endDate,
    markets: event.markets?.map(marketForFormatter),
  }));
};
