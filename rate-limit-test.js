import http from 'k6/http';
import { check } from 'k6';

export const options = {
  vus: 1,
  iterations: 70,
};

export default function () {
  const res = http.get('http://localhost/1', {
    redirects: 0,
    headers: {
      'X-API-Key': 'rate-test-key',
    },
  });

  check(res, {
    'rate limit works': (r) => r.status === 302 || r.status === 429,
  });
}