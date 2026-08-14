import * as React from "react"
import { cn } from "@/lib/utils"

/**
 * A table that becomes a list of cards on a phone.
 *
 * With real data these tables are 750–1100px wide inside a 390px screen. They
 * scrolled sideways, which is technically fine and unusable in practice: you
 * see the first two columns of each row and have to drag to read the rest.
 *
 * Each cell is stamped with the text of its column header, and CSS below sm
 * turns every row into a card of label/value pairs. The stamping is done from
 * the DOM rather than through a column-definition API because that way all
 * twenty-one existing tables get it without being rewritten — and a table
 * whose header changes stays correct without anyone remembering to update a
 * parallel list.
 */
const Table = React.forwardRef<
    HTMLTableElement,
    React.HTMLAttributes<HTMLTableElement>
>(({ className, ...props }, ref) => {
    const inner = React.useRef<HTMLTableElement | null>(null)

    React.useEffect(() => {
        const table = inner.current
        if (!table) return

        const stamp = () => {
            const heads = Array.from(table.querySelectorAll("thead th"))
                .map(th => (th.textContent || "").trim())
            table.querySelectorAll("tbody tr").forEach(row => {
                Array.from(row.children).forEach((cell, i) => {
                    // An empty label is the signal for "no prefix": checkbox and
                    // action columns have no header text and want none shown.
                    cell.setAttribute("data-label", heads[i] ?? "")
                })
            })
        }
        stamp()

        // Rows arrive after the first render, and change on every filter,
        // sort and page. Restamping on mutation keeps the labels honest.
        const observer = new MutationObserver(stamp)
        observer.observe(table, { childList: true, subtree: true })
        return () => observer.disconnect()
    })

    return (
        <div className="relative w-full overflow-auto">
            <table
                ref={node => {
                    inner.current = node
                    if (typeof ref === "function") ref(node)
                    else if (ref) ref.current = node
                }}
                className={cn("responsive-table w-full caption-bottom text-sm", className)}
                {...props}
            />
        </div>
    )
})
Table.displayName = "Table"

const TableHeader = React.forwardRef<
    HTMLTableSectionElement,
    React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
    <thead ref={ref} className={cn("[&_tr]:border-b", className)} {...props} />
))
TableHeader.displayName = "TableHeader"

const TableBody = React.forwardRef<
    HTMLTableSectionElement,
    React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
    <tbody
        ref={ref}
        className={cn("[&_tr:last-child]:border-0", className)}
        {...props}
    />
))
TableBody.displayName = "TableBody"

const TableFooter = React.forwardRef<
    HTMLTableSectionElement,
    React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
    <tfoot
        ref={ref}
        className={cn(
            "border-t bg-muted/50 font-medium [&>tr]:last:border-b-0",
            className
        )}
        {...props}
    />
))
TableFooter.displayName = "TableFooter"

const TableRow = React.forwardRef<
    HTMLTableRowElement,
    React.HTMLAttributes<HTMLTableRowElement>
>(({ className, ...props }, ref) => (
    <tr
        ref={ref}
        className={cn(
            "border-b transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted",
            className
        )}
        {...props}
    />
))
TableRow.displayName = "TableRow"

const TableHead = React.forwardRef<
    HTMLTableCellElement,
    React.ThHTMLAttributes<HTMLTableCellElement>
>(({ className, ...props }, ref) => (
    <th
        ref={ref}
        className={cn(
            "h-12 px-4 text-left align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0",
            className
        )}
        {...props}
    />
))
TableHead.displayName = "TableHead"

const TableCell = React.forwardRef<
    HTMLTableCellElement,
    React.TdHTMLAttributes<HTMLTableCellElement>
>(({ className, ...props }, ref) => (
    <td
        ref={ref}
        className={cn("p-4 align-middle [&:has([role=checkbox])]:pr-0", className)}
        {...props}
    />
))
TableCell.displayName = "TableCell"

const TableCaption = React.forwardRef<
    HTMLTableCaptionElement,
    React.HTMLAttributes<HTMLTableCaptionElement>
>(({ className, ...props }, ref) => (
    <caption
        ref={ref}
        className={cn("mt-4 text-sm text-muted-foreground", className)}
        {...props}
    />
))
TableCaption.displayName = "TableCaption"

export {
    Table,
    TableHeader,
    TableBody,
    TableFooter,
    TableHead,
    TableRow,
    TableCell,
    TableCaption,
}
