import { ArrowLeft } from 'lucide-react';
import { Link } from 'react-router-dom';

import { Button } from '@/components/ui/button';

export function BackButton({ to, label }: { to: string; label: string }) {
  return (
    <Button variant="outline" size="sm" nativeButton={false} render={<Link to={to} />} className="w-fit">
      <ArrowLeft />
      {label}
    </Button>
  );
}
