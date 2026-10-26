// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

#pragma once

#include <bpfcore/utils.h>

#include <common/connection_info.h>

#include <gotracer/go_common.h>

#include <maps/outgoing_trace_map.h>

// Publishes the client span as the parent of the server side of the connection: on this
// host through trace_map, across hosts through outgoing_trace_map
static __always_inline void publish_client_trace(const connection_info_t *conn, tp_info_t *tp) {
    tp_info_pid_t tp_p = {
        .pid = pid_from_pid_tgid(bpf_get_current_pid_tgid()),
        .valid = 1,
        .req_type = k_event_type_tcp_request,
    };
    tp_clone(&tp_p.tp, tp);
    tp_p.tp.flags = 1;
    tp_p.tp.ts = bpf_ktime_get_ns();

    connection_info_t sorted_conn = *conn;
    sort_connection_info(&sorted_conn);
    set_trace_info_for_connection(&sorted_conn, TRACE_TYPE_CLIENT, &tp_p);

    egress_key_t e_key = {
        .d_port = sorted_conn.d_port,
        .s_port = sorted_conn.s_port,
    };
    sort_egress_key(&e_key);
    bpf_map_update_elem(&outgoing_trace_map, &e_key, &tp_p, BPF_ANY);
}

// Removes what publish_client_trace stored once the request has its reply
static __always_inline void unpublish_client_trace(const connection_info_t *conn) {
    connection_info_t sorted_conn = *conn;
    sort_connection_info(&sorted_conn);
    delete_trace_info_for_connection(&sorted_conn, TRACE_TYPE_CLIENT);

    egress_key_t e_key = {
        .d_port = sorted_conn.d_port,
        .s_port = sorted_conn.s_port,
    };
    sort_egress_key(&e_key);
    bpf_map_delete_elem(&outgoing_trace_map, &e_key);
}
