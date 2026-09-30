import { Navigate, useLocation } from 'react-router-dom';

/** Old /keys links, including ?key=<id>, keep working after the section became /users. */
export function KeysRedirect() {
  const { search } = useLocation();
  const params = new URLSearchParams(search);
  const id = params.get('key');
  if (id) {
    params.delete('key');
    params.set('user', id);
  }
  const query = params.toString();
  return <Navigate to={`/users${query ? `?${query}` : ''}`} replace />;
}
