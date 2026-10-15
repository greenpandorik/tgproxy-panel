import { Users } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';

import { useKeys } from '@/api/keys';
import { Panel, PanelHeader } from '@/components/common/Panel';
import { Button } from '@/components/ui/button';
import { formatNumber } from '@/lib/format';
import { peopleNow, sharedByPeople } from '@/pages/users/presence';

const SHOWN = 8;

/** The shared users with how many people are on each now; nothing when there are none. */
export function SharedPeopleCard() {
  const { t, i18n } = useTranslation();
  const query = useKeys({ type: 'SHARED', per_page: 200 });
  const num = (v: number) => formatNumber(v, i18n.language);
  const users = sharedByPeople(query.data?.items ?? []);
  if (users.length === 0) return null;
  const shown = users.slice(0, SHOWN);

  return (
    <Panel>
      <PanelHeader
        icon={Users}
        title={t('monitoring.shared_title')}
        actions={
          users.length > SHOWN ? (
            <Button variant="outline" size="sm" nativeButton={false} render={<Link to="/users?type=SHARED" />}>
              {t('monitoring.shared_all', { value: num(users.length) })}
            </Button>
          ) : undefined
        }
      />
      <ul className="divide-y divide-hairline">
        {shown.map((u) => {
          const people = peopleNow(u);
          return (
            <li key={u.id}>
              <Link
                to={`/users?user=${u.id}`}
                className="flex items-baseline justify-between gap-4 px-(--panel-x) py-2.5 hover:bg-elevated"
              >
                <span className="min-w-0 truncate text-body text-foreground">{u.label}</span>
                <span className="shrink-0 text-right">
                  <span className="mono block text-mono text-foreground">
                    {t('common.approx', { value: num(people), context: people === 0 ? 'zero' : undefined })}
                  </span>
                  {u.live.online && (
                    <span className="mono block text-micro text-mute">
                      {t('common.connections_count', { value: num(u.live.connections) })}
                    </span>
                  )}
                </span>
              </Link>
            </li>
          );
        })}
      </ul>
    </Panel>
  );
}
