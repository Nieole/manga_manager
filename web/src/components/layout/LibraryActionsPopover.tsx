import type { ReactNode } from 'react';
import { AnchoredPopover } from '../ui/AnchoredPopover';

interface LibraryActionsPopoverProps {
    anchor: HTMLButtonElement;
    label: string;
    onClose: () => void;
    children: ReactNode;
}

export function LibraryActionsPopover(props: LibraryActionsPopoverProps) {
    return (
        <AnchoredPopover
            {...props}
            className="w-48 bg-gray-800 border border-gray-700 rounded-lg shadow-xl"
        />
    );
}
