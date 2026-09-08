_bootwright_complete() {
    local line item previous current i
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
        [[ $line == :* ]] && continue
        if [[ $current != --* && $line == --*=* ]]; then
            line=${line#*=}
        fi
        COMPREPLY+=("$line")
    done < <("${COMP_WORDS[0]}" @PROTOCOL@ "${request[@]}" 2>/dev/null)
}
complete -F _bootwright_complete bootwright
