import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';
import { Counter } from 'k6/metrics';

const base = __ENV.BASE_URL || 'http://localhost:8080';
const profile = __ENV.PROFILE || 'normal';
const profiles = {
  normal: { executor: 'constant-vus', vus: 5, duration: '30s' },
  spike: { executor: 'ramping-vus', startVUs: 0, stages: [
    { duration: '2s', target: 40 }, { duration: '10s', target: 40 }, { duration: '5s', target: 0 },
  ] },
  last: { executor: 'shared-iterations', vus: 20, iterations: 20, maxDuration: '15s' },
};
if (!profiles[profile]) throw new Error('PROFILE must be normal, spike or last');

const successfulBookings = new Counter('successful_bookings');

export const options = {
  scenarios: { tickets: profiles[profile] },
  thresholds: {
    checks: ['rate==1'],
    http_req_duration: ['p(95)<300'],
    ...(profile === 'last' ? { successful_bookings: ['count==1'] } : {}),
  },
};

export function setup() {
  const catalog = http.get(base + '/api/events');
  if (catalog.status !== 200) throw new Error('Catalog unavailable');
  if (profile === 'last' && !catalog.json().some(e => e.id === 'last-seat' && e.remaining === 1)) {
    throw new Error('The last-seat demo event must have one available seat');
  }
  return { run: Date.now() + '-' + Math.random() };
}

export default function ({ run }) {
  const catalog = http.get(base + '/api/events', { tags: { name: 'catalog' } });
  check(catalog, { 'catalog returned 200': response => response.status === 200 });
  const iteration = exec.scenario.iterationInTest;
  if (profile === 'normal' && iteration % 5 !== 0) return;

  const expected = profile === 'last' ? [201, 409] : [201];
  const result = http.post(base + '/api/bookings',
    JSON.stringify({ event_id: profile === 'last' ? 'last-seat' : 'night-jazz', quantity: 1 }),
    {
      headers: { 'Content-Type': 'application/json', 'Idempotency-Key': run + '-' + iteration },
      tags: { name: 'create-booking' },
      responseCallback: http.expectedStatuses(...expected),
    });
  check(result, { 'booking returned an expected status': response => expected.includes(response.status) });
  successfulBookings.add(result.status === 201 ? 1 : 0);
  if (result.status === 201 && profile !== 'last') {
    const cancelled = http.del(base + '/api/bookings/' + result.json('id'), null, { tags: { name: 'cancel-booking' } });
    check(cancelled, { 'booking cancelled': response => response.status === 200 });
  }
}
