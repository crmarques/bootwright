_bootwright_complete() {
    local line item previous current i directive=0
    local -a request=()
    COMPREPLY=()
    for ((i=1; i<=COMP_CWORD; i++)); do
        item=${COMP_WORDS[i]}
        if [[ $item == = && ${#request[@]} -gt 0 ]]; then
            previous=$((${#request[@]} - 1))
            request[previous]+="="
            if ((i < COMP_CWORD)); then
                ((i++))
                request[previous]+=${COMP_WORDS[i]}
            fi
        else
            request+=("$item")
        fi
    done
    current=${COMP_WORDS[COMP_CWORD]}
    while IFS= read -r line; do
        if [[ $line == :* ]]; then
            directive=${line#:}
            continue
        fi
        if [[ $current != --* && $line == --*=* ]]; then
            line=${line#*=}
        fi
        COMPREPLY+=("$line")
    done < <("${COMP_WORDS[0]}" @PROTOCOL@ "${request[@]}" 2>/dev/null)
    if (( directive & 2 )); then
        compopt -o nospace 2>/dev/null || true
    else
        compopt +o nospace 2>/dev/null || true
    fi
}
complete -F _bootwright_complete bootwright
