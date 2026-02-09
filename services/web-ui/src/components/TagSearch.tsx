import * as React from "react"
import { Check, ChevronsUpDown, Search } from "lucide-react"

import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import {
    Popover,
    PopoverContent,
    PopoverTrigger,
} from "@/components/ui/popover"
import { Tag } from "@/types"

interface TagSearchProps {
    tags: Tag[]
    onSelect: (tag: Tag) => void
    selectedTags: number[]
}

export function TagSearch({ tags, onSelect, selectedTags }: TagSearchProps) {
    const [open, setOpen] = React.useState(false)
    const [searchQuery, setSearchQuery] = React.useState("")
    const listRef = React.useRef<HTMLDivElement>(null)

    const filteredTags = React.useMemo(() => {
        if (!searchQuery) return tags;
        const lowerQuery = searchQuery.toLowerCase();
        return tags.filter(tag =>
            (tag.alias && tag.alias.toLowerCase().includes(lowerQuery)) ||
            (tag.code && tag.code.toLowerCase().includes(lowerQuery)) ||
            tag.id.toString().includes(lowerQuery)
        );
    }, [tags, searchQuery]);

    const handleSelect = (tag: Tag) => {
        console.log("Selecting tag:", tag.id);
        onSelect(tag);
        setOpen(false);
        setSearchQuery("");
    };

    return (
        <Popover open={open} onOpenChange={setOpen}>
            <PopoverTrigger asChild>
                <Button
                    variant="outline"
                    role="combobox"
                    aria-expanded={open}
                    className="w-full justify-between"
                    disabled={tags.length === 0}
                >
                    <span className="truncate">
                        {tags.length === 0 ? "No tags available" : "Select tag to add..."}
                    </span>
                    <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
                </Button>
            </PopoverTrigger>
            <PopoverContent className="w-[400px] p-0" align="start">
                <div className="flex flex-col max-h-[400px]">
                    <div className="flex items-center border-b px-3">
                        <Search className="mr-2 h-4 w-4 shrink-0 opacity-50" />
                        <input
                            className="flex h-11 w-full rounded-md bg-transparent py-3 text-sm outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-50"
                            placeholder="Search tag..."
                            value={searchQuery}
                            onChange={(e) => setSearchQuery(e.target.value)}
                            autoFocus
                        />
                    </div>

                    <div
                        ref={listRef}
                        className="overflow-y-auto overflow-x-hidden p-1"
                    >
                        {filteredTags.length === 0 ? (
                            <div className="py-6 text-center text-sm text-muted-foreground">
                                No tag found.
                            </div>
                        ) : (
                            <div className="flex flex-col gap-0.5">
                                <div className="px-2 py-1.5 text-xs font-medium text-muted-foreground">
                                    Available Tags
                                </div>
                                {filteredTags.map((tag) => {
                                    const isSelected = selectedTags.includes(tag.id)
                                    return (
                                        <div
                                            key={tag.id}
                                            onClick={() => handleSelect(tag)}
                                            className={cn(
                                                "relative flex cursor-pointer select-none items-center rounded-sm px-2 py-1.5 text-sm outline-none hover:bg-accent hover:text-accent-foreground",
                                                isSelected && "opacity-70"
                                            )}
                                        >
                                            <div className="flex flex-col flex-1 mr-2">
                                                <span className="font-medium">{tag.alias || "Unnamed Tag"}</span>
                                                <span className="text-xs text-muted-foreground">{tag.code} • {tag.data_type}</span>
                                            </div>
                                            {isSelected && <Check className="h-4 w-4 ml-auto" />}
                                        </div>
                                    )
                                })}
                            </div>
                        )}
                    </div>
                </div>
            </PopoverContent>
        </Popover>
    )
}
