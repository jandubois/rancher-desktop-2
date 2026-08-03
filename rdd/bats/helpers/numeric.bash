#!/usr/bin/env bash

# assert_output_ge - Assert that the integer value of $output is greater than or equal to expected
assert_output_ge() {
    local -i expected=$1
    local -i actual

    if [[ ${output} =~ ^-?[0-9]+$ ]]; then
        actual="${output}"
    else
        # shellcheck disable=SC2312 # We're intentionally failing anyway.
        batslib_print_kv_single_or_multi 8 \
            'expected' ">= ${expected}" \
            'actual' "${output} (not a valid integer)" |
            batslib_decorate 'output does not contain a valid integer' |
            fail
        return $?
    fi

    if ((actual < expected)); then
        # shellcheck disable=SC2312 # We're intentionally failing anyway.
        batslib_print_kv_single_or_multi 8 \
            'expected' ">= ${expected}" \
            'actual' "${actual}" |
            batslib_decorate 'output is less than expected minimum' |
            fail
    fi
}

# assert_output_lt - Assert that the integer value of $output is less than expected
assert_output_lt() {
    local -i expected=$1
    local -i actual

    if [[ ${output} =~ ^-?[0-9]+$ ]]; then
        actual="${output}"
    else
        # shellcheck disable=SC2312 # We're intentionally failing anyway.
        batslib_print_kv_single_or_multi 8 \
            'expected' "< ${expected}" \
            'actual' "${output} (not a valid integer)" |
            batslib_decorate 'output does not contain a valid integer' |
            fail
        return $?
    fi

    if ((actual >= expected)); then
        # shellcheck disable=SC2312 # We're intentionally failing anyway.
        batslib_print_kv_single_or_multi 8 \
            'expected' "< ${expected}" \
            'actual' "${actual}" |
            batslib_decorate 'output is not less than expected maximum' |
            fail
    fi
}

# quantity_to_bytes - Print the byte count of a Kubernetes resource quantity
# Only the binary forms rdd emits are accepted; anything else is a bug worth failing on.
quantity_to_bytes() { # <quantity>
    local -i exponent

    if [[ ! $1 =~ ^([0-9]+)(Ki|Mi|Gi|Ti)?$ ]]; then
        # shellcheck disable=SC2312 # We're intentionally failing anyway.
        batslib_print_kv_single_or_multi 8 \
            'expected' 'an integer with an optional binary suffix, e.g. 16Gi' \
            'actual' "$1" |
            batslib_decorate 'not a binary resource quantity' |
            fail
        return $?
    fi

    case ${BASH_REMATCH[2]} in
    Ki) exponent=1 ;;
    Mi) exponent=2 ;;
    Gi) exponent=3 ;;
    Ti) exponent=4 ;;
    *) exponent=0 ;; # No suffix, so the quantity is already a byte count.
    esac

    echo $((BASH_REMATCH[1] * (1024 ** exponent)))
}
